package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Visible consumes the approved case-owner batch contract, not case tables.
// The provider validates current trusted identity, tenant and case permission,
// then returns a JSON array containing only requested, visible inbox UUIDs.
// Inactive but still authorized inboxes remain visible for historical editing.
// Both input/output are <=500, no NULL, duplicates or foreign additions. It is
// read-only on this caller snapshot: no DDL, transaction control or external IO.
func (c InboxContract) Visible(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if tx == nil || c.schema == "" {
		return nil, ErrOwnerContractUnavailable
	}
	if tenant != c.session.TenantID {
		return nil, ErrDecisionAuthorizationDenied
	}
	if len(ids) > 500 {
		return nil, ErrProjectionLimit
	}
	ordered := append([]uuid.UUID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	requested := make(map[uuid.UUID]bool, len(ids))
	values := make([]string, len(ids))
	for i, id := range ordered {
		if id == uuid.Nil || requested[id] {
			return nil, ErrTenantScope
		}
		requested[id] = true
		values[i] = id.String()
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var document mysqlstore.JSON
	var size sql.NullInt64
	// NO_MERGE materializes the single bounded provider result once, so byte
	// checking never transfers an unbounded document or repeats authorization.
	query := "SELECT /*+ NO_MERGE(v) */ OCTET_LENGTH(visible),CASE WHEN OCTET_LENGTH(visible)<=20000 THEN visible ELSE 'null' END FROM (SELECT " + c.schema + ".case_manager_visible_decision_inboxes_v1(?,?,?,?,?) AS visible) v"
	if err := tx.QueryRowContext(ctx, query, mysqlstore.UUID(c.session.ActorID), mysqlstore.UUID(tenant), mysqlstore.UUID(c.session.SessionID), c.session.MembershipVersion, mysqlstore.JSON(raw)).Scan(&size, &document); err != nil {
		return nil, errors.Join(ErrOwnerContractUnavailable, err)
	}
	if !size.Valid {
		return nil, ErrOwnerContractUnavailable
	}
	if size.Int64 > 20000 || size.Int64 < 0 {
		return nil, ErrProjectionLimit
	}
	var visible []string
	if err := json.Unmarshal(document, &visible); err != nil || visible == nil {
		return nil, ErrOwnerContractUnavailable
	}
	if len(visible) > len(ids) {
		return nil, ErrOwnerRelationshipConflict
	}
	result := make(map[uuid.UUID]bool, len(visible))
	for _, value := range visible {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || !requested[id] || result[id] {
			return nil, ErrOwnerRelationshipConflict
		}
		result[id] = true
	}
	return result, nil
}
