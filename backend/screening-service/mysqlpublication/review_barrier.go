package mysqlpublication

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// DecisionReviewReason is the screening owner's bounded authoritative read
// contract. Call in the case-authorized review transaction. Projectors lock
// source before effects, so execution reads must not acquire effect row locks.
func DecisionReviewReason(ctx context.Context, tx *sql.Tx, database string, tenant, decision uuid.UUID) (string, error) {
	schema, e := mysqlstore.Identifier(database)
	if e != nil || tx == nil || tenant == uuid.Nil || decision == uuid.Nil {
		return "", ErrInvalid
	}
	rows, e := tx.QueryContext(ctx, "SELECT id FROM "+schema+".decision_engine_screening_executions WHERE tenant_id=? AND decision_id=? ORDER BY id LIMIT 101", mysqlstore.UUID(tenant), mysqlstore.UUID(decision))
	if e != nil {
		return "", e
	}
	executions := map[uuid.UUID]bool{}
	for rows.Next() {
		var id mysqlstore.UUID
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return "", e
		}
		executions[uuid.UUID(id)] = false
	}
	e = rows.Err()
	closeErr := rows.Close()
	if e != nil {
		return "", e
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(executions) > 100 {
		return "", fmt.Errorf("screening review limit exceeded")
	}
	if len(executions) == 0 {
		return "", nil
	}
	type node struct {
		status            string
		partial, archived bool
	}
	nodes := map[uuid.UUID]node{}
	roots := []uuid.UUID{}
	replaced := map[uuid.UUID]uuid.UUID{}
	rows, e = tx.QueryContext(ctx, "SELECT id,execution_id,status,partial,is_archived FROM "+schema+".screening_screenings WHERE tenant_id=? AND decision_id=? AND execution_id IS NOT NULL ORDER BY id LIMIT 101 FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(decision))
	if e != nil {
		return "", e
	}
	for rows.Next() {
		var id, exec mysqlstore.UUID
		var n node
		if e = rows.Scan(&id, &exec, &n.status, &n.partial, &n.archived); e != nil {
			rows.Close()
			return "", e
		}
		roots = append(roots, uuid.UUID(id))
		executions[uuid.UUID(exec)] = true
		nodes[uuid.UUID(id)] = n
	}
	e = rows.Err()
	closeErr = rows.Close()
	if e != nil {
		return "", e
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(roots) > 100 {
		return "", ErrInvalid
	}
	rows, e = tx.QueryContext(ctx, "SELECT r.source_id,n.id,n.status,n.partial,n.is_archived FROM "+schema+".screening_refinements r JOIN "+schema+".screening_screenings root ON root.tenant_id=r.tenant_id AND root.id=r.root_id JOIN "+schema+".screening_screenings n ON n.tenant_id=r.tenant_id AND n.id=r.screening_id WHERE r.tenant_id=? AND root.decision_id=? AND r.applied_at IS NOT NULL ORDER BY n.id LIMIT 101 FOR SHARE OF n", mysqlstore.UUID(tenant), mysqlstore.UUID(decision))
	if e != nil {
		return "", e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 100 {
			return "", ErrInvalid
		}
		var parent, id mysqlstore.UUID
		var n node
		if e = rows.Scan(&parent, &id, &n.status, &n.partial, &n.archived); e != nil {
			return "", e
		}
		replaced[uuid.UUID(parent)] = uuid.UUID(id)
		nodes[uuid.UUID(id)] = n
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	reason := ""
	for _, id := range roots {
		for depth := 0; replaced[id] != uuid.Nil; depth++ {
			if depth >= 20 {
				return "", ErrInvalid
			}
			id = replaced[id]
		}
		n := nodes[id]
		if n.status == "confirmed_hit" {
			return "A screening has a confirmed match. This decision cannot be approved.", nil
		}
		if reason == "" {
			switch {
			case n.partial:
				reason = "Screening results are incomplete. Refine the search before approving."
			case n.archived:
				reason = "Archived screening evidence must be resolved before approving."
			case n.status == "failed":
				reason = "A screening failed. Resolve it before approving."
			case n.status != "no_hit":
				reason = "Screening matches must be resolved before approving."
			}
		}
	}
	for _, seen := range executions {
		if !seen && reason == "" {
			reason = "Screening evidence is still pending. Wait for screening to complete before approving."
		}
	}
	return reason, nil
}
