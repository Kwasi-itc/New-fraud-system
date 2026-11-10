package mysqlcase

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// RecordScreeningReview is the case-owned SQL history contract. Screening calls
// it on the same transaction after persisting its immutable review receipt.
// It does not let a caller supply arbitrary case event contents.
func RecordScreeningReview(ctx context.Context, tx *sql.Tx, database string, tenant, operation, actor, session uuid.UUID, version int64) error {
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return e
	}
	if tx == nil || tenant == uuid.Nil || operation == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return errors.New("verified screening review identity required")
	}
	var roles int64
	if e = tx.QueryRowContext(ctx, "SELECT "+schema+".identity_session_roles_v1(?,?,?,?,TRUE)", mysqlstore.UUID(actor), mysqlstore.UUID(tenant), mysqlstore.UUID(session), version).Scan(&roles); e != nil {
		return e
	}
	if roles&6 == 0 {
		return errors.New("case review access denied")
	}
	var caseID, resource mysqlstore.UUID
	var previous, status, findings string
	e = tx.QueryRowContext(ctx, "SELECT case_id,resource_id,previous_status,status,findings FROM "+schema+".screening_reviews WHERE tenant_id=? AND operation_id=? AND actor_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(operation), mysqlstore.UUID(actor)).Scan(&caseID, &resource, &previous, &status, &findings)
	if e != nil {
		return e
	}
	var active, member, linked bool
	var caseStatus string
	e = tx.QueryRowContext(ctx, "SELECT c.status,i.active,EXISTS(SELECT 1 FROM "+schema+".case_manager_inbox_users u WHERE u.tenant_id=c.tenant_id AND u.inbox_id=c.inbox_id AND u.user_id=?),EXISTS(SELECT 1 FROM "+schema+".case_manager_case_screenings l WHERE l.tenant_id=c.tenant_id AND l.case_id=c.id AND l.match_id=?) FROM "+schema+".case_manager_cases c JOIN "+schema+".case_manager_inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE c.tenant_id=? AND c.id=? FOR SHARE OF c,i", mysqlstore.UUID(actor), resource, mysqlstore.UUID(tenant), caseID).Scan(&caseStatus, &active, &member, &linked)
	if e != nil {
		return e
	}
	if !active || caseStatus == "closed" || !linked || (roles&2 == 0 && !member) {
		return errors.New("current active case access required")
	}
	payload, e := json.Marshal(map[string]any{"operation_id": operation, "resource_id": uuid.UUID(resource), "actor_id": actor, "owner": "screening", "previous_status": previous, "status": status, "findings": findings})
	if e != nil {
		return e
	}
	event := mysqlstore.UUID(uuid.NewSHA1(operation, []byte("case-screening-review/v1")))
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_case_events(id,tenant_id,case_id,event_type,payload,created_at) VALUES(?,?,?,'investigator_review_recorded',?,UTC_TIMESTAMP(6))", event, mysqlstore.UUID(tenant), caseID, mysqlstore.JSON(payload)); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES(?,?,'case',?,'investigator_review_recorded',?,'pending',UTC_TIMESTAMP(6))", event, mysqlstore.UUID(tenant), caseID, mysqlstore.JSON(payload))
	return e
}
