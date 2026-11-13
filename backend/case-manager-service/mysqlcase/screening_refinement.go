package mysqlcase

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

// RecordScreeningRefinement derives all links/history from the immutable
// screening-owned receipt on the caller's transaction. Original evidence stays.
func RecordScreeningRefinement(ctx context.Context, tx *sql.Tx, database string, tenant, operation, actor, session uuid.UUID, version int64) error {
	schema, e := mysqlstore.Identifier(database)
	if e != nil || tx == nil || actor == uuid.Nil || session == uuid.Nil {
		return errors.New("verified refinement identity required")
	}
	var roles int64
	if e = tx.QueryRowContext(ctx, "SELECT "+schema+".identity_session_roles_v1(?,?,?,?,TRUE)", mysqlstore.UUID(actor), mysqlstore.UUID(tenant), mysqlstore.UUID(session), version).Scan(&roles); e != nil {
		return e
	}
	if roles&6 == 0 {
		return errors.New("case review access required")
	}
	var caseID, source, replacement mysqlstore.UUID
	var findings string
	e = tx.QueryRowContext(ctx, "SELECT case_id,source_id,screening_id,findings FROM "+schema+".screening_refinements WHERE tenant_id=? AND id=? AND actor_id=? AND applied_at IS NOT NULL", mysqlstore.UUID(tenant), mysqlstore.UUID(operation), mysqlstore.UUID(actor)).Scan(&caseID, &source, &replacement, &findings)
	if e != nil {
		return e
	}
	var active, linked, member bool
	var caseStatus string
	e = tx.QueryRowContext(ctx, "SELECT c.status,i.active,EXISTS(SELECT 1 FROM "+schema+".case_manager_inbox_users u WHERE u.tenant_id=c.tenant_id AND u.inbox_id=c.inbox_id AND u.user_id=?),EXISTS(SELECT 1 FROM "+schema+".case_manager_case_screenings l WHERE l.tenant_id=c.tenant_id AND l.case_id=c.id AND l.screening_id=?) FROM "+schema+".case_manager_cases c JOIN "+schema+".case_manager_inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE c.tenant_id=? AND c.id=? FOR SHARE OF c,i", mysqlstore.UUID(actor), source, mysqlstore.UUID(tenant), caseID).Scan(&caseStatus, &active, &member, &linked)
	if e != nil {
		return e
	}
	if !active || caseStatus == "closed" || !linked || (roles&2 == 0 && !member) {
		return errors.New("current case access required")
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,status FROM "+schema+".screening_matches WHERE tenant_id=? AND screening_id=? ORDER BY id LIMIT 51", mysqlstore.UUID(tenant), replacement)
	if e != nil {
		return e
	}
	type candidate struct {
		id     mysqlstore.UUID
		status string
	}
	matches := []candidate{}
	for rows.Next() {
		var m candidate
		if e = rows.Scan(&m.id, &m.status); e != nil {
			rows.Close()
			return e
		}
		matches = append(matches, m)
	}
	e = rows.Err()
	closeErr := rows.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if len(matches) > 50 {
		return errors.New("refinement result limit exceeded")
	}
	values := []string{}
	args := []any{}
	add := func(match any, status string) {
		values = append(values, "(?,?,?,?,?,?,UTC_TIMESTAMP(6))")
		args = append(args, mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), caseID, replacement, match, status)
	}
	if len(matches) == 0 {
		add(nil, "no_hit")
	}
	for _, m := range matches {
		add(m.id, m.status)
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_case_screenings(id,tenant_id,case_id,screening_id,match_id,status,created_at) VALUES"+strings.Join(values, ","), args...); e != nil {
		return e
	}
	payload, _ := json.Marshal(map[string]any{"operation_id": operation, "source_id": uuid.UUID(source), "screening_id": uuid.UUID(replacement), "actor_id": actor, "findings": findings})
	event := mysqlstore.UUID(uuid.NewSHA1(operation, []byte("screening-refinement/v1")))
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_case_events(id,tenant_id,case_id,event_type,payload,created_at) VALUES(?,?,?,'screening_refined',?,UTC_TIMESTAMP(6))", event, mysqlstore.UUID(tenant), caseID, mysqlstore.JSON(payload)); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES(?,?,'case',?,'screening_refined',?,'pending',UTC_TIMESTAMP(6))", event, mysqlstore.UUID(tenant), caseID, mysqlstore.JSON(payload))
	return e
}
