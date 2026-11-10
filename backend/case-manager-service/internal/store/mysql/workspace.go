package mysql

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type workspaceRepo struct{ scope }

func (r workspaceRepo) HasLink(ctx context.Context, t, i uuid.UUID, kind string, resource uuid.UUID) (bool, error) {
	table, col := "case_decisions", "decision_id"
	if kind == "screenings" {
		table, col = "case_screenings", "screening_id"
	} else if kind != "decisions" {
		return false, c.Invalid("invalid evidence kind")
	}
	var ok bool
	e := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.table(table)+" WHERE tenant_id=? AND case_id=? AND "+col+"=?)", id(t), id(i), id(resource)).Scan(&ok)
	return ok, e
}
func (r workspaceRepo) where(t uuid.UUID, f c.WorkspaceFilters) (string, []any, error) {
	name := f.Name
	f.Name = ""
	q, a, e := r.caseWhere(t, f.CaseFilters)
	if e != nil {
		return "", nil, e
	}
	add := func(term string, v any) { q += " AND " + term; a = append(a, v) }
	if name != "" {
		add("LOWER(c.name) LIKE ? ESCAPE '!'", "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(name))+"%")
	}
	if f.Overdue {
		q += " AND c.status<>'closed' AND EXISTS(SELECT 1 FROM " + r.table("inboxes") + " i WHERE i.tenant_id=c.tenant_id AND i.id=c.inbox_id AND DATE_ADD(c.created_at,INTERVAL i.sla_days DAY)<=CURRENT_TIMESTAMP(6))"
	}
	if f.Unassigned {
		q += " AND c.assigned_to IS NULL"
	}
	if f.CreatedFrom != nil {
		add("c.created_at>=?", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		add("c.created_at<?", *f.CreatedTo)
	}
	if f.ReviewLevel != "" {
		add("c.review_level=?", f.ReviewLevel)
	}
	if f.TagID != nil {
		add("EXISTS(SELECT 1 FROM "+r.table("case_tags")+" l WHERE l.tenant_id=c.tenant_id AND l.case_id=c.id AND l.tag_id=? AND l.deleted_at IS NULL)", id(*f.TagID))
	}
	if f.DecisionID != nil {
		add("EXISTS(SELECT 1 FROM "+r.table("case_decisions")+" l WHERE l.tenant_id=c.tenant_id AND l.case_id=c.id AND l.decision_id=?)", id(*f.DecisionID))
	}
	if f.RelatedTo != nil {
		q += " AND c.id<>? AND EXISTS(SELECT 1 FROM " + r.table("case_decisions") + " d JOIN " + r.table("case_decisions") + " source ON source.tenant_id=d.tenant_id AND source.case_id=? AND((OCTET_LENGTH(source.pivot_value)>0 AND source.pivot_hash=d.pivot_hash AND source.pivot_value=d.pivot_value) OR(source.object_type=d.object_type AND source.object_id=d.object_id AND OCTET_LENGTH(source.object_id)>0)) WHERE d.tenant_id=c.tenant_id AND d.case_id=c.id)"
		a = append(a, id(*f.RelatedTo), id(*f.RelatedTo))
	}
	return q, a, nil
}
func (r workspaceRepo) Queue(ctx context.Context, t uuid.UUID, f c.WorkspaceFilters, n int) (c.QueuePage, error) {
	out := c.QueuePage{Cases: []c.Case{}, Counts: map[string]int64{}}
	n = bounded(n)
	where, a, e := r.where(t, f)
	if e != nil {
		return out, e
	}
	rows, e := r.db.QueryContext(ctx, "SELECT c.status,COUNT(*) FROM "+r.table("cases")+" c WHERE "+where+" GROUP BY c.status", a...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var kind string
		var count int64
		if e = rows.Scan(&kind, &count); e != nil {
			rows.Close()
			return out, e
		}
		out.Counts[kind] = count
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if f.Before != nil {
		where += " AND((c.boost_reason IS NOT NULL)<? OR((c.boost_reason IS NOT NULL)=? AND(c.created_at<? OR(c.created_at=? AND c.id<?))))"
		a = append(a, f.Before.Boosted, f.Before.Boosted, f.Before.At, f.Before.At, id(f.Before.ID))
	}
	a = append(a, n+1)
	columns := prefixColumns(caseCols, "c")
	columns = columns[:len(columns)-len("c.sla_due_at")] + "DATE_ADD(c.created_at,INTERVAL i.sla_days DAY)"
	rows, e = r.db.QueryContext(ctx, "SELECT "+columns+" FROM "+r.table("cases")+" c JOIN "+r.table("inboxes")+" i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE "+where+" ORDER BY(c.boost_reason IS NOT NULL) DESC,c.created_at DESC,c.id DESC LIMIT ?", a...)
	out.Cases, e = collect(rows, e, scanCase)
	if e != nil {
		return out, e
	}
	if len(out.Cases) > n {
		out.Cases = out.Cases[:n]
		v := out.Cases[n-1]
		out.NextCursor = (c.Cursor{ID: v.ID, At: v.CreatedAt, Boosted: v.BoostReason != nil}).Encode()
	}
	return out, nil
}
func (r workspaceRepo) Links(ctx context.Context, t, i uuid.UUID, kind string, before *c.Cursor, n int) (c.LinkPage, error) {
	out := c.LinkPage{Items: []json.RawMessage{}}
	n = bounded(n)
	var table, cols string
	var scan func(row) (any, uuid.UUID, time.Time, error)
	switch kind {
	case "decisions":
		table, cols = "case_decisions", decisionCols
		scan = func(r row) (any, uuid.UUID, time.Time, error) {
			v, e := scanDecision(r)
			return v, v.ID, v.CreatedAt, e
		}
	case "screenings":
		table, cols = "case_screenings", screeningCols
		scan = func(r row) (any, uuid.UUID, time.Time, error) {
			v, e := scanScreening(r)
			return v, v.ID, v.CreatedAt, e
		}
	case "files":
		table, cols = "case_files", fileCols
		scan = func(r row) (any, uuid.UUID, time.Time, error) { v, e := scanFile(r); return v, v.ID, v.CreatedAt, e }
	case "events":
		table, cols = "case_events", "id,tenant_id,case_id,event_type,payload,created_at"
		scan = func(r row) (any, uuid.UUID, time.Time, error) {
			var v c.Event
			var b []byte
			var u, t, i uuid.UUID
			var kind string
			var at time.Time
			e := r.Scan(uid(&u), uid(&t), uid(&i), &kind, &b, &at)
			if e == nil {
				e = json.Unmarshal(b, &v)
			}
			v.ID, v.TenantID, v.CaseID, v.EventType, v.CreatedAt = u, t, i, kind, at
			return v, u, at, e
		}
	default:
		return out, c.Invalid("invalid relationship")
	}
	q := "SELECT " + cols + " FROM " + r.table(table) + " WHERE tenant_id=? AND case_id=?"
	a := []any{id(t), id(i)}
	if before != nil {
		q += " AND(created_at<? OR(created_at=? AND id<?))"
		a = append(a, before.At, before.At, id(before.ID))
	}
	a = append(a, n+1)
	rows, e := r.db.QueryContext(ctx, q+" ORDER BY created_at DESC,id DESC LIMIT ?", a...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	var last c.Cursor
	for rows.Next() {
		v, key, at, e := scan(rows)
		if e != nil {
			return out, e
		}
		if len(out.Items) == n {
			out.NextCursor = last.Encode()
			break
		}
		b, e := json.Marshal(v)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, b)
		last = c.Cursor{ID: key, At: at}
	}
	return out, rows.Err()
}

type selectionRepo struct{ scope }

func (r selectionRepo) Lookup(ctx context.Context, t uuid.UUID, ids []uuid.UUID, actor string, admin bool) ([]c.DecisionSelection, error) {
	out := make([]c.DecisionSelection, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 500 {
		return nil, c.Invalid("too many decisions")
	}
	u, e := uuid.Parse(actor)
	if e != nil {
		return nil, c.ErrForbidden
	}
	a := []any{admin, id(u), id(t)}
	for _, v := range ids {
		a = append(a, id(v))
	}
	rows, e := r.db.QueryContext(ctx, "SELECT d.id,l.case_id,c.name,c.status,(? OR EXISTS(SELECT 1 FROM "+r.table("inbox_users")+" m WHERE m.tenant_id=d.tenant_id AND m.inbox_id=c.inbox_id AND m.user_id=?)) FROM "+r.schema+".decision_engine_decisions d LEFT JOIN "+r.table("case_decisions")+" l ON l.tenant_id=d.tenant_id AND l.decision_id=d.id LEFT JOIN "+r.table("cases")+" c ON c.tenant_id=l.tenant_id AND c.id=l.case_id WHERE d.tenant_id=? AND d.id IN("+placeholders(len(ids))+")", a...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	found := map[uuid.UUID]c.DecisionSelection{}
	for rows.Next() {
		var v c.DecisionSelection
		var allowed bool
		e = rows.Scan(uid(&v.DecisionID), nullableID{&v.CaseID}, &v.CaseName, &v.CaseStatus, &allowed)
		if e != nil {
			return nil, e
		}
		if v.CaseID == nil {
			v.State = "unlinked"
		} else if allowed {
			v.State = "linked"
		} else {
			v.State = "unavailable"
			v.CaseID, v.CaseName, v.CaseStatus = nil, nil, nil
		}
		found[v.DecisionID] = v
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	for j, v := range ids {
		item, ok := found[v]
		if !ok {
			item = c.DecisionSelection{DecisionID: v, State: "unavailable"}
		}
		out[j] = item
	}
	return out, nil
}
