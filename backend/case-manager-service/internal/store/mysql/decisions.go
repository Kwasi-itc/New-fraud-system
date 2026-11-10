package mysql

import (
	"context"
	"crypto/sha256"
	"errors"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type decisionRepo struct{ scope }

const decisionCols = "id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_value,created_at"

func scanDecision(r row) (c.DecisionLink, error) {
	var v c.DecisionLink
	var pivot []byte
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.CaseID), uid(&v.DecisionID), nullableID{&v.ScenarioID}, &v.ObjectType, &v.ObjectID, &pivot, &v.CreatedAt)
	if len(pivot) > 0 {
		p := string(pivot)
		v.PivotValue = &p
	}
	return v, normalized(e)
}
func (r decisionRepo) Create(ctx context.Context, v c.DecisionLink) (c.DecisionLink, error) {
	pivot := ""
	if v.PivotValue != nil {
		pivot = *v.PivotValue
	}
	hash := sha256.Sum256([]byte(pivot))
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("case_decisions")+"(id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_hash,pivot_value,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.CaseID), id(v.DecisionID), idp(v.ScenarioID), []byte(v.ObjectType), []byte(v.ObjectID), hash[:], []byte(pivot), v.CreatedAt)
	if e != nil {
		return c.DecisionLink{}, normalized(e)
	}
	return v, nil
}
func (r decisionRepo) Membership(ctx context.Context, t, i uuid.UUID) (*c.DecisionLink, error) {
	v, e := scanDecision(r.db.QueryRowContext(ctx, "SELECT "+decisionCols+" FROM "+r.table("case_decisions")+" WHERE tenant_id=? AND decision_id=?", id(t), id(i)))
	if errors.Is(e, c.ErrNotFound) {
		return nil, nil
	}
	return &v, e
}
func (r decisionRepo) ListByCase(ctx context.Context, t, i uuid.UUID) ([]c.DecisionLink, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT "+decisionCols+" FROM "+r.table("case_decisions")+" WHERE tenant_id=? AND case_id=? ORDER BY created_at DESC,id DESC LIMIT 501", id(t), id(i))
	return collectLimited(rows, e, scanDecision, 500)
}
func (r decisionRepo) FindCase(ctx context.Context, t, i uuid.UUID) (*c.Case, error) {
	link, e := r.Membership(ctx, t, i)
	if e != nil || link == nil {
		return nil, e
	}
	v, e := (caseRepo{r.scope}).Get(ctx, t, link.CaseID)
	return &v, e
}
func (r decisionRepo) FindOpenByPivot(ctx context.Context, t uuid.UUID, inbox *uuid.UUID, pivot string) (*c.Case, error) {
	hash := sha256.Sum256([]byte(pivot))
	q := "SELECT " + prefixColumns(caseCols, "c") + " FROM " + r.table("cases") + " c WHERE c.tenant_id=? AND c.status<>'closed' AND EXISTS(SELECT 1 FROM " + r.table("case_decisions") + " d WHERE d.tenant_id=c.tenant_id AND d.case_id=c.id AND d.pivot_hash=? AND d.pivot_value=?) AND EXISTS(SELECT 1 FROM " + r.table("inboxes") + " i WHERE i.tenant_id=c.tenant_id AND i.id=c.inbox_id AND i.active)"
	a := []any{id(t), hash[:], []byte(pivot)}
	if inbox != nil {
		q += " AND c.inbox_id=?"
		a = append(a, id(*inbox))
	}
	v, e := scanCase(r.db.QueryRowContext(ctx, q+" ORDER BY c.created_at DESC,c.id DESC LIMIT 1 FOR UPDATE", a...))
	if errors.Is(e, c.ErrNotFound) {
		return nil, nil
	}
	return &v, e
}
func (r decisionRepo) Transfer(ctx context.Context, t, d, from, to uuid.UUID) error {
	result, e := r.db.ExecContext(ctx, "UPDATE "+r.table("case_decisions")+" SET case_id=? WHERE tenant_id=? AND decision_id=? AND case_id=?", id(to), id(t), id(d), id(from))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return c.ErrConflict
	}
	_, e = r.db.ExecContext(ctx, "UPDATE "+r.table("case_screenings")+" l JOIN "+r.schema+".screening_screenings s ON s.tenant_id=l.tenant_id AND s.id=l.screening_id SET l.case_id=? WHERE l.tenant_id=? AND l.case_id=? AND s.decision_id=?", id(to), id(t), id(from), id(d))
	return e
}
