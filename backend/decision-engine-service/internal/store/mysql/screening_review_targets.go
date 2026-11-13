package mysql

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type screeningReviewTarget struct {
	CaseID, SourceID string
	Response         map[string]any
}

// All decision case inboxes have already been authorized by ScreeningSummaries.
// Two bounded queries supply source/case links and applied refinement leaves.
func readScreeningReviewTargets(ctx context.Context, tx *sql.Tx, schema string, tenant, decision uuid.UUID) (map[string]screeningReviewTarget, error) {
	out := map[string]screeningReviewTarget{}
	roots := map[string]string{}
	rows, e := tx.QueryContext(ctx, "SELECT s.id,s.execution_id,MIN(l.case_id),s.status,s.partial,s.match_count FROM "+schema+".screening_screenings s LEFT JOIN "+schema+".case_manager_case_screenings l ON l.tenant_id=s.tenant_id AND l.screening_id=s.id WHERE s.tenant_id=? AND s.decision_id=? AND s.execution_id IS NOT NULL GROUP BY s.id,s.execution_id,s.status,s.partial,s.match_count ORDER BY s.id LIMIT 101", mysqlstore.UUID(tenant), mysqlstore.UUID(decision))
	if e != nil {
		return nil, e
	}
	count := 0
	for rows.Next() {
		count++
		if count > 100 {
			rows.Close()
			return nil, ErrProjectionLimit
		}
		var source, execution mysqlstore.UUID
		var caseID mysqlstore.NullUUID
		var status string
		var partial bool
		var matches int
		if e = rows.Scan(&source, &execution, &caseID, &status, &partial, &matches); e != nil {
			rows.Close()
			return nil, e
		}
		target := screeningReviewTarget{SourceID: uuid.UUID(source).String(), Response: map[string]any{"status": status, "partial": partial, "match_count": matches}}
		if caseID.Valid {
			target.CaseID = uuid.UUID(caseID.UUID).String()
		}
		out[uuid.UUID(execution).String()] = target
		roots[uuid.UUID(source).String()] = uuid.UUID(execution).String()
	}
	e = rows.Err()
	ce := rows.Close()
	if e != nil {
		return nil, e
	}
	if ce != nil {
		return nil, ce
	}
	rows, e = tx.QueryContext(ctx, "SELECT r.root_id,r.source_id,r.screening_id,r.case_id,n.status,n.partial,n.match_count FROM "+schema+".screening_refinements r JOIN "+schema+".screening_screenings root ON root.tenant_id=r.tenant_id AND root.id=r.root_id JOIN "+schema+".screening_screenings n ON n.tenant_id=r.tenant_id AND n.id=r.screening_id WHERE r.tenant_id=? AND root.decision_id=? ORDER BY r.created_at,r.id LIMIT 101", mysqlstore.UUID(tenant), mysqlstore.UUID(decision))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	count = 0
	for rows.Next() {
		count++
		if count > 100 {
			return nil, ErrProjectionLimit
		}
		var root, source, replacement, caseID mysqlstore.UUID
		var status string
		var partial bool
		var matches int
		if e = rows.Scan(&root, &source, &replacement, &caseID, &status, &partial, &matches); e != nil {
			return nil, e
		}
		key := roots[uuid.UUID(root).String()]
		if key != "" {
			out[key] = screeningReviewTarget{CaseID: uuid.UUID(caseID).String(), SourceID: uuid.UUID(replacement).String(), Response: map[string]any{"status": status, "partial": partial, "match_count": matches, "refined": true}}
		}
	}
	return out, rows.Err()
}
