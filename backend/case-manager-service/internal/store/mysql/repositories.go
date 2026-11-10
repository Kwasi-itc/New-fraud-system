package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type queryable interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type scope struct {
	db     queryable
	schema string
}
type row interface{ Scan(...any) error }

func id(v uuid.UUID) mysqlstore.UUID { return mysqlstore.UUID(v) }
func idp(v *uuid.UUID) any {
	if v == nil {
		return nil
	}
	return id(*v)
}

type nullableID struct{ v **uuid.UUID }

func (n nullableID) Scan(v any) error {
	if v == nil {
		*n.v = nil
		return nil
	}
	var u mysqlstore.UUID
	if e := u.Scan(v); e != nil {
		return e
	}
	next := uuid.UUID(u)
	*n.v = &next
	return nil
}
func uid(v *uuid.UUID) *mysqlstore.UUID { return (*mysqlstore.UUID)(v) }
func normalized(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return c.ErrNotFound
	}
	var d *driver.MySQLError
	if errors.As(e, &d) && d.Number == 1062 {
		return c.ErrConflict
	}
	if errors.As(e, &d) && d.Number == 1644 && d.Message == "case assignment capacity reached" {
		return c.ErrConflict
	}
	return e
}
func bounded(n int) int {
	if n < 1 || n > 500 {
		return 100
	}
	return n
}
func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func collect[T any](rows *sql.Rows, e error, scan func(row) (T, error)) ([]T, error) {
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func collectLimited[T any](rows *sql.Rows, e error, scan func(row) (T, error), limit int) ([]T, error) {
	items, e := collect(rows, e, scan)
	if e != nil {
		return nil, e
	}
	if len(items) > limit {
		return nil, c.Invalid("relationship exceeds the response limit; use the paginated workspace links or narrow the selection")
	}
	return items, nil
}
func (s scope) table(t string) string { return s.schema + ".case_manager_" + t }

// CheckIdentity consults the shared identity owner contract. A valid signature
// alone does not keep a revoked session or stale membership usable.
func CheckIdentity(ctx context.Context, db queryable, database string, p access.Principal, lock bool) error {
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return e
	}
	actor, e := uuid.Parse(p.Subject)
	if e != nil || p.Kind != access.User || p.SessionID == uuid.Nil || p.AuthzVersion < 1 {
		return c.ErrForbidden
	}
	var roles uint64
	e = db.QueryRowContext(ctx, "SELECT "+schema+".identity_session_roles_v1(?,?,?,?,?)", id(actor), id(p.TenantID), id(p.SessionID), p.AuthzVersion, lock).Scan(&roles)
	if e != nil {
		return e
	}
	if roles&6 == 0 || p.Admin != (roles&2 != 0) {
		return c.ErrForbidden
	}
	return nil
}

type UnitOfWork struct {
	DB       *sql.DB
	Database string
}

func (u UnitOfWork) WithinTransaction(ctx context.Context, fn func(ports.Repositories) error) error {
	if fn == nil {
		return errors.New("case transaction callback required")
	}
	tx, e := u.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if p, ok := access.FromContext(ctx); ok && p.Kind == access.User {
		if e = CheckIdentity(ctx, tx, u.Database, p, true); e != nil {
			return e
		}
	}
	repos, e := NewRepositories(tx, u.Database)
	if e != nil {
		return e
	}
	if e = fn(repos); e != nil {
		return normalized(e)
	}
	return tx.Commit()
}

// NewRepositories binds every repository to the same connection or transaction.
func NewRepositories(db queryable, database string) (ports.Repositories, error) {
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return ports.Repositories{}, e
	}
	if db == nil {
		return ports.Repositories{}, errors.New("case database required")
	}
	s := scope{db, schema}
	return ports.Repositories{Inboxes: inboxRepo{s}, Cases: caseRepo{s}, Tags: tagRepo{s}, Events: eventRepo{s}, Files: fileRepo{s}, Uploads: uploadRepo{s}, Reports: reportRepo{s}, Memberships: memberRepo{s}, Contributors: contributorRepo{s}, Intake: intakeRepo{s}, Decisions: decisionRepo{s}, Screenings: screeningRepo{s}, References: referenceRepo{s}, Workspace: workspaceRepo{s}, Selections: selectionRepo{s}, Analytics: analyticsRepo{s}, Explorer: explorerRepo{s}}, nil
}

type inboxRepo struct{ scope }

const inboxCols = "id,tenant_id,name,IF(active,'active','archived'),escalation_inbox_id,auto_assign_enabled,case_review_manual,case_review_on_case_created,case_review_on_escalate,created_at,updated_at,sla_days"

func scanInbox(r row) (c.Inbox, error) {
	var v c.Inbox
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), &v.Name, &v.Status, nullableID{&v.EscalationInboxID}, &v.AutoAssignEnabled, &v.CaseReviewManual, &v.CaseReviewOnCaseCreated, &v.CaseReviewOnEscalate, &v.CreatedAt, &v.UpdatedAt, &v.SLADays)
	return v, normalized(e)
}
func (r inboxRepo) get(ctx context.Context, t, i uuid.UUID, lock string) (c.Inbox, error) {
	return scanInbox(r.db.QueryRowContext(ctx, "SELECT "+inboxCols+" FROM "+r.table("inboxes")+" WHERE tenant_id=? AND id=?"+lock, id(t), id(i)))
}
func (r inboxRepo) Get(ctx context.Context, t, i uuid.UUID) (c.Inbox, error) {
	return r.get(ctx, t, i, "")
}
func (r inboxRepo) LockShared(ctx context.Context, t, i uuid.UUID) (c.Inbox, error) {
	return r.get(ctx, t, i, " FOR SHARE")
}
func (r inboxRepo) LockUpdate(ctx context.Context, t, i uuid.UUID) (c.Inbox, error) {
	return r.get(ctx, t, i, " FOR UPDATE")
}
func (r inboxRepo) Create(ctx context.Context, v c.Inbox) (c.Inbox, error) {
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("inboxes")+"(id,tenant_id,name,active,escalation_inbox_id,auto_assign_enabled,case_review_manual,case_review_on_case_created,case_review_on_escalate,created_at,updated_at,sla_days) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), v.Name, v.Status == "active", idp(v.EscalationInboxID), v.AutoAssignEnabled, v.CaseReviewManual, v.CaseReviewOnCaseCreated, v.CaseReviewOnEscalate, v.CreatedAt, v.UpdatedAt, v.SLADays)
	if e != nil {
		return c.Inbox{}, normalized(e)
	}
	return r.Get(ctx, v.TenantID, v.ID)
}
func (r inboxRepo) Update(ctx context.Context, v c.Inbox) (c.Inbox, error) {
	_, e := r.db.ExecContext(ctx, "UPDATE "+r.table("inboxes")+" SET name=?,active=?,escalation_inbox_id=?,auto_assign_enabled=?,case_review_manual=?,case_review_on_case_created=?,case_review_on_escalate=?,updated_at=?,sla_days=? WHERE tenant_id=? AND id=?", v.Name, v.Status == "active", idp(v.EscalationInboxID), v.AutoAssignEnabled, v.CaseReviewManual, v.CaseReviewOnCaseCreated, v.CaseReviewOnEscalate, v.UpdatedAt, v.SLADays, id(v.TenantID), id(v.ID))
	if e != nil {
		return c.Inbox{}, e
	}
	return r.Get(ctx, v.TenantID, v.ID)
}
func (r inboxRepo) List(ctx context.Context, t uuid.UUID, user string) ([]c.Inbox, error) {
	q := "SELECT " + inboxCols + " FROM " + r.table("inboxes") + " i WHERE tenant_id=?"
	a := []any{id(t)}
	if user != "" {
		u, e := uuid.Parse(user)
		if e != nil {
			return nil, c.ErrForbidden
		}
		q += " AND EXISTS(SELECT 1 FROM " + r.table("inbox_users") + " m WHERE m.tenant_id=i.tenant_id AND m.inbox_id=i.id AND m.user_id=?)"
		a = append(a, id(u))
	}
	rows, e := r.db.QueryContext(ctx, q+" ORDER BY created_at DESC,id DESC LIMIT 501", a...)
	return collectLimited(rows, e, scanInbox, 500)
}

type caseRepo struct{ scope }

const caseCols = "id,tenant_id,inbox_id,name,status,outcome,type,assigned_to,snoozed_until,boost_reason,review_level,created_at,updated_at,sla_due_at"

func scanCase(r row) (c.Case, error) {
	var v c.Case
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.InboxID), &v.Name, &v.Status, &v.Outcome, &v.Type, &v.AssignedTo, &v.SnoozedUntil, &v.BoostReason, &v.ReviewLevel, &v.CreatedAt, &v.UpdatedAt, &v.SLADueAt)
	return v, normalized(e)
}
func (r caseRepo) get(ctx context.Context, t, i uuid.UUID, lock string) (c.Case, error) {
	return scanCase(r.db.QueryRowContext(ctx, "SELECT "+caseCols+" FROM "+r.table("cases")+" WHERE tenant_id=? AND id=?"+lock, id(t), id(i)))
}
func (r caseRepo) Get(ctx context.Context, t, i uuid.UUID) (c.Case, error) {
	return r.get(ctx, t, i, "")
}
func (r caseRepo) Lock(ctx context.Context, t, i uuid.UUID) (c.Case, error) {
	return r.get(ctx, t, i, " FOR UPDATE")
}
func (r caseRepo) LockShared(ctx context.Context, t, i uuid.UUID) (c.Case, error) {
	return r.get(ctx, t, i, " FOR SHARE")
}
func (r caseRepo) Create(ctx context.Context, v c.Case) (c.Case, error) {
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("cases")+"("+caseCols+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.InboxID), v.Name, v.Status, v.Outcome, v.Type, v.AssignedTo, v.SnoozedUntil, v.BoostReason, v.ReviewLevel, v.CreatedAt, v.UpdatedAt, v.SLADueAt)
	if e != nil {
		return c.Case{}, normalized(e)
	}
	return r.Get(ctx, v.TenantID, v.ID)
}
func (r caseRepo) Update(ctx context.Context, v c.Case) (c.Case, error) {
	_, e := r.db.ExecContext(ctx, "UPDATE "+r.table("cases")+" SET inbox_id=?,name=?,status=?,outcome=?,type=?,assigned_to=?,snoozed_until=?,boost_reason=?,review_level=?,updated_at=?,sla_due_at=? WHERE tenant_id=? AND id=?", id(v.InboxID), v.Name, v.Status, v.Outcome, v.Type, v.AssignedTo, v.SnoozedUntil, v.BoostReason, v.ReviewLevel, v.UpdatedAt, v.SLADueAt, id(v.TenantID), id(v.ID))
	if e != nil {
		return c.Case{}, e
	}
	return r.Get(ctx, v.TenantID, v.ID)
}
func (r caseRepo) Assign(ctx context.Context, t, i uuid.UUID, user *string, at time.Time) error {
	result, e := r.db.ExecContext(ctx, "UPDATE "+r.table("cases")+" SET assigned_to=?,updated_at=? WHERE tenant_id=? AND id=?", user, at, id(t), id(i))
	return r.existingMutation(ctx, t, i, result, e)
}
func (r caseRepo) Snooze(ctx context.Context, t, i uuid.UUID, until *time.Time, at time.Time) error {
	result, e := r.db.ExecContext(ctx, "UPDATE "+r.table("cases")+" SET snoozed_until=?,updated_at=? WHERE tenant_id=? AND id=?", until, at, id(t), id(i))
	return r.existingMutation(ctx, t, i, result, e)
}
func (r caseRepo) existingMutation(ctx context.Context, t, i uuid.UUID, result sql.Result, e error) error {
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 0 {
		return e
	}
	var key mysqlstore.UUID
	return normalized(r.db.QueryRowContext(ctx, "SELECT id FROM "+r.table("cases")+" WHERE tenant_id=? AND id=?", id(t), id(i)).Scan(&key))
}
func (s scope) caseWhere(t uuid.UUID, f c.CaseFilters) (string, []any, error) {
	q := "c.tenant_id=?"
	a := []any{id(t)}
	if f.AccessUserID != "" {
		u, e := uuid.Parse(f.AccessUserID)
		if e != nil {
			return "", nil, c.ErrForbidden
		}
		q += " AND EXISTS(SELECT 1 FROM " + s.table("inbox_users") + " m WHERE m.tenant_id=c.tenant_id AND m.inbox_id=c.inbox_id AND m.user_id=?)"
		a = append(a, id(u))
	}
	if len(f.Statuses) > 0 {
		q += " AND c.status IN(" + placeholders(len(f.Statuses)) + ")"
		for _, v := range f.Statuses {
			a = append(a, string(v))
		}
	}
	if len(f.InboxIDs) > 0 {
		q += " AND c.inbox_id IN(" + placeholders(len(f.InboxIDs)) + ")"
		for _, v := range f.InboxIDs {
			a = append(a, id(v))
		}
	}
	if f.Name != "" {
		q += " AND LOWER(c.name) LIKE ?"
		a = append(a, "%"+strings.ToLower(f.Name)+"%")
	}
	if !f.IncludeSnoozed {
		q += " AND(c.snoozed_until IS NULL OR c.snoozed_until<=CURRENT_TIMESTAMP(6))"
	}
	if f.AssigneeID != "" {
		q += " AND c.assigned_to=?"
		a = append(a, f.AssigneeID)
	}
	return q, a, nil
}
func (r caseRepo) List(ctx context.Context, t uuid.UUID, f c.CaseFilters, n int) ([]c.Case, error) {
	q, a, e := r.caseWhere(t, f)
	if e != nil {
		return nil, e
	}
	a = append(a, bounded(n))
	rows, e := r.db.QueryContext(ctx, "SELECT "+prefixColumns(caseCols, "c")+" FROM "+r.table("cases")+" c WHERE "+q+" ORDER BY c.boost_reason IS NULL,c.created_at DESC,c.id DESC LIMIT ?", a...)
	return collect(rows, e, scanCase)
}
func prefixColumns(cols, alias string) string {
	v := strings.Split(cols, ",")
	for i := range v {
		v[i] = alias + "." + v[i]
	}
	return strings.Join(v, ",")
}
