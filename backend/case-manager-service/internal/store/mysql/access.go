package mysql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type memberRepo struct{ scope }

func (r memberRepo) eligible(ctx context.Context, t, u uuid.UUID) (bool, error) {
	var ok bool
	e := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.schema+".identity_memberships m JOIN "+r.schema+".identity_users u ON u.id=m.user_id JOIN "+r.schema+".identity_tenants t ON t.id=m.tenant_id WHERE m.tenant_id=? AND m.user_id=? AND m.active AND u.active AND t.active AND((m.roles+0)&6)<>0)", id(t), id(u)).Scan(&ok)
	return ok, e
}
func (r memberRepo) Has(ctx context.Context, t, i uuid.UUID, user string) (bool, error) {
	u, e := uuid.Parse(user)
	if e != nil {
		return false, c.ErrForbidden
	}
	ok, e := r.eligible(ctx, t, u)
	if e != nil || !ok {
		return false, e
	}
	e = r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.table("inbox_users")+" WHERE tenant_id=? AND inbox_id=? AND user_id=?)", id(t), id(i), id(u)).Scan(&ok)
	return ok, e
}
func scanMember(r row) (c.InboxUser, error) {
	var v c.InboxUser
	var user uuid.UUID
	var key []byte
	e := r.Scan(&key, uid(&v.TenantID), uid(&v.InboxID), uid(&user), &v.AutoAssignEnabled, &v.CreatedAt, &v.Capacity)
	if e != nil {
		return v, e
	}
	v.UserID = user.String()
	if key != nil {
		if e = uid(&v.ID).Scan(key); e != nil {
			return v, e
		}
	}
	return v, nil
}
func (r memberRepo) List(ctx context.Context, t, i uuid.UUID) ([]c.InboxUser, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT id,tenant_id,inbox_id,user_id,auto_assign_enabled,created_at,capacity FROM "+r.table("inbox_users")+" WHERE tenant_id=? AND inbox_id=? ORDER BY user_id LIMIT 1001", id(t), id(i))
	return collectLimited(rows, e, scanMember, 1000)
}
func (r memberRepo) Put(ctx context.Context, v c.InboxUser) (c.InboxUser, error) {
	u, e := uuid.Parse(v.UserID)
	if e != nil {
		return c.InboxUser{}, c.Invalid("invalid user ID")
	}
	ok, e := r.eligible(ctx, v.TenantID, u)
	if e != nil {
		return c.InboxUser{}, e
	}
	if !ok {
		return c.InboxUser{}, c.Invalid("assignee must be an active investigator")
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("inbox_users")+"(tenant_id,inbox_id,user_id,auto_assign_enabled,created_at,capacity) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE auto_assign_enabled=?,capacity=?", id(v.TenantID), id(v.InboxID), id(u), v.AutoAssignEnabled, v.CreatedAt, v.Capacity, v.AutoAssignEnabled, v.Capacity)
	if e != nil {
		return c.InboxUser{}, e
	}
	return scanMember(r.db.QueryRowContext(ctx, "SELECT id,tenant_id,inbox_id,user_id,auto_assign_enabled,created_at,capacity FROM "+r.table("inbox_users")+" WHERE tenant_id=? AND inbox_id=? AND user_id=?", id(v.TenantID), id(v.InboxID), id(u)))
}
func (r memberRepo) Remove(ctx context.Context, t, i uuid.UUID, user string) error {
	u, e := uuid.Parse(user)
	if e != nil {
		return c.Invalid("invalid user ID")
	}
	var assigned bool
	e = r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.table("cases")+" WHERE tenant_id=? AND inbox_id=? AND assigned_to=? AND status<>'closed')", id(t), id(i), user).Scan(&assigned)
	if e != nil {
		return e
	}
	if assigned {
		return c.Invalid("member has assigned open cases")
	}
	result, e := r.db.ExecContext(ctx, "DELETE FROM "+r.table("inbox_users")+" WHERE tenant_id=? AND inbox_id=? AND user_id=?", id(t), id(i), id(u))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n == 0 {
		return c.ErrNotFound
	}
	return e
}
func (r memberRepo) RecordChange(ctx context.Context, t, i uuid.UUID, actor *string, kind, user string, auto bool, at time.Time, capacity int) error {
	if actor == nil {
		return c.ErrForbidden
	}
	u, e := uuid.Parse(*actor)
	if e != nil {
		return c.ErrForbidden
	}
	event := uuid.New()
	b, e := json.Marshal(map[string]any{"user_id": user, "auto_assign_enabled": auto, "capacity": capacity})
	if e != nil {
		return e
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("inbox_events")+"(id,tenant_id,inbox_id,user_id,event_type,payload,created_at) VALUES(?,?,?,?,?,?,?)", id(event), id(t), id(i), id(u), kind, mysqlstore.JSON(b), at)
	if e != nil {
		return e
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("inbox_outbox")+"(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES(?,?,'inbox',?,?,?,'pending',?)", id(event), id(t), id(i), kind, mysqlstore.JSON(b), at)
	return e
}

type contributorRepo struct{ scope }

func (r contributorRepo) Add(ctx context.Context, t, i uuid.UUID, user string, at time.Time) error {
	u, e := uuid.Parse(user)
	if e != nil {
		return c.ErrForbidden
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("contributors")+"(tenant_id,case_id,user_id,created_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE user_id=user_id", id(t), id(i), id(u), at)
	return e
}
func (r contributorRepo) List(ctx context.Context, t, i uuid.UUID) ([]c.Contributor, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT a.user_id,u.display_name,a.created_at FROM "+r.table("contributors")+" a JOIN "+r.schema+".identity_users u ON u.id=a.user_id WHERE a.tenant_id=? AND a.case_id=? ORDER BY a.created_at,a.user_id LIMIT 501", id(t), id(i))
	return collectLimited(rows, e, func(r row) (c.Contributor, error) {
		var v c.Contributor
		var user uuid.UUID
		e := r.Scan(uid(&user), &v.Name, &v.CreatedAt)
		v.UserID = user.String()
		return v, e
	}, 500)
}
func (r contributorRepo) ResolveNames(ctx context.Context, t uuid.UUID, users []string) (map[string]string, error) {
	out := map[string]string{}
	if len(users) == 0 {
		return out, nil
	}
	if len(users) > 1000 {
		return nil, c.Invalid("too many users")
	}
	a := []any{id(t)}
	for _, v := range users {
		u, e := uuid.Parse(v)
		if e != nil {
			return nil, c.Invalid("invalid user ID")
		}
		a = append(a, id(u))
	}
	rows, e := r.db.QueryContext(ctx, "SELECT u.id,u.display_name FROM "+r.schema+".identity_users u JOIN "+r.schema+".identity_memberships m ON m.user_id=u.id WHERE m.tenant_id=? AND u.id IN("+placeholders(len(users))+")", a...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var u uuid.UUID
		var name string
		if e = rows.Scan(uid(&u), &name); e != nil {
			return nil, e
		}
		out[u.String()] = name
	}
	return out, rows.Err()
}

type intakeRepo struct{ scope }

func (r intakeRepo) Lock(ctx context.Context, t uuid.UUID, s, key string) error {
	b, e := json.Marshal([]string{"case-manager-intake", t.String(), s, key})
	if e != nil {
		return e
	}
	h := sha256.Sum256(b)
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("workflow_locks")+"(tenant_id,lock_hash) VALUES(?,?) ON DUPLICATE KEY UPDATE lock_hash=lock_hash", id(t), h[:])
	return e
}
func (r intakeRepo) Get(ctx context.Context, t uuid.UUID, s string, i uuid.UUID) (*ports.IntakeReceipt, error) {
	return readReceipt(ctx, r.db, r.schema, t, s, i)
}
func (r intakeRepo) Create(ctx context.Context, v ports.IntakeReceipt) error {
	h, e := hex.DecodeString(v.PayloadHash)
	if e != nil || len(h) != 32 {
		return c.Invalid("invalid receipt hash")
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("intake_receipts")+"(tenant_id,source,event_id,payload_hash,case_id,created_at) VALUES(?,?,?,?,?,?)", id(v.TenantID), []byte(v.Source), id(v.EventID), h, id(v.CaseID), v.CreatedAt)
	return normalized(e)
}
