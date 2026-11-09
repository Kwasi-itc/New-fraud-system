package mysqlauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var denied = platform.ErrDenied
var conflict = platform.ErrConflict

type Store struct {
	DB       *sql.DB
	schema   string
	database string
	role     string
}

var _ platform.HTTPStore = (*Store)(nil)

func New(db *sql.DB, database string) (*Store, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	if db == nil {
		return nil, errors.New("auth database required")
	}
	return &Store{DB: db, schema: s, database: database}, nil
}
func (s *Store) table(name string) string { return s.schema + "." + name }
func id(v uuid.UUID) mysqlstore.UUID      { return mysqlstore.UUID(v) }
func secret() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func rolesValid(v []string) bool {
	if len(v) < 1 || len(v) > 7 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range v {
		if seen[r] || !slices.Contains([]string{"tenant_admin", "case_manager_admin", "case_investigator", "platform_viewer", "platform_builder", "platform_publisher", "platform_admin"}, r) {
			return false
		}
		seen[r] = true
	}
	return true
}
func rolesList(v string) []string {
	if v == "" {
		return []string{}
	}
	return strings.Split(v, ",")
}
func noRow(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return denied
	}
	return err
}
func (s *Store) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Ready(ctx context.Context) error {
	steps, e := Statements("fraud")
	if e != nil {
		return e
	}
	var applied, dirty int
	e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(state<>'applied'),0) FROM "+s.table("identity_auth_schema_migrations")).Scan(&applied, &dirty)
	if e != nil {
		return e
	}
	if applied != len(steps) || dirty != 0 {
		return errors.New("auth migration lineage incomplete")
	}
	var baseline int
	if e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table("identity_schema_migrations")+" WHERE state='applied'").Scan(&baseline); e != nil {
		return e
	}
	if baseline != 14 {
		return errors.New("identity contract incomplete")
	}
	return s.verifyRole(ctx)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) session(ctx context.Context, q queryer, raw string) (platform.Session, error) {
	if !sessioncredential.Valid(raw) {
		return platform.Session{}, denied
	}
	var v platform.Session
	var sid, user, tenant mysqlstore.UUID
	var roles string
	e := q.QueryRowContext(ctx, "SELECT s.id,s.user_id,s.tenant_id,u.display_name,m.roles,m.version,s.expires_at FROM "+s.table("identity_sessions")+" s JOIN "+s.table("identity_users")+" u ON u.id=s.user_id JOIN "+s.table("identity_memberships")+" m ON m.user_id=s.user_id AND m.tenant_id=s.tenant_id JOIN "+s.table("identity_tenants")+" t ON t.id=s.tenant_id WHERE s.digest=? AND s.revoked_at IS NULL AND s.expires_at>SYSDATE(6) AND s.idle_until>SYSDATE(6) AND u.active AND m.active AND t.active", sessioncredential.Digest(raw)).Scan(&sid, &user, &tenant, &v.Name, &roles, &v.Version, &v.Expires)
	if e != nil {
		return v, noRow(e)
	}
	v.ID, v.UserID, v.TenantID = uuid.UUID(sid), uuid.UUID(user), uuid.UUID(tenant)
	v.Roles = rolesList(roles)
	return v, nil
}
func (s *Store) lockAdmin(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_auth_admin_guards")+"(tenant_id) VALUES(?) ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id)", id(tenant))
	return e
}

// Lock identity rows in user -> membership -> session -> tenant order. Expiry
// and roles are read again after every blocking lock has been acquired.
func (s *Store) caller(ctx context.Context, tx *sql.Tx, raw string, write, admin bool) (platform.Session, error) {
	return s.lockCaller(ctx, tx, raw, write, admin, false)
}
func (s *Store) lockCaller(ctx context.Context, tx *sql.Tx, raw string, write, admin, renewal bool) (platform.Session, error) {
	v, e := s.session(ctx, tx, raw)
	if e != nil {
		return v, e
	}
	if admin && write {
		if e = s.lockAdmin(ctx, tx, v.TenantID); e != nil {
			return v, e
		}
	}
	var live bool
	if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_users")+" WHERE id=? FOR SHARE", id(v.UserID)).Scan(&live); e != nil || !live {
		if e != nil {
			return v, noRow(e)
		}
		return v, denied
	}
	lock := " FOR SHARE"
	if write && !renewal {
		lock = " FOR UPDATE"
	}
	if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_memberships")+" WHERE tenant_id=? AND user_id=?"+lock, id(v.TenantID), id(v.UserID)).Scan(&live); e != nil {
		return v, noRow(e)
	}
	var ro bool
	if write {
		lock = " FOR UPDATE"
	}
	if e = tx.QueryRowContext(ctx, "SELECT read_only FROM "+s.table("identity_sessions")+" WHERE id=?"+lock, id(v.ID)).Scan(&ro); e != nil {
		return v, noRow(e)
	}
	if write && ro && !renewal {
		return v, denied
	}
	if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_tenants")+" WHERE id=? FOR SHARE", id(v.TenantID)).Scan(&live); e != nil {
		return v, noRow(e)
	}
	v, e = s.session(ctx, tx, raw)
	if e == nil && admin && !slices.Contains(v.Roles, "tenant_admin") {
		e = denied
	}
	return v, e
}
func (s *Store) audit(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, actor, action string, target uuid.UUID, details any) error {
	b, e := json.Marshal(details)
	if e != nil {
		return e
	}
	if len(b) > 16384 {
		return errors.New("audit details bound exceeded")
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_audit_events")+"(id,tenant_id,actor_id,action,target_id,details) VALUES(?,?,?,?,?,?)", id(uuid.New()), id(tenant), actor, action, id(target), mysqlstore.JSON(b))
	return e
}
func identityKey(issuer, subject string) ([32]byte, error) {
	if issuer == "" || len(issuer) > 2048 || strings.TrimSpace(subject) == "" || len(subject) > 1024 {
		return [32]byte{}, denied
	}
	b, _ := json.Marshal([]string{issuer, subject})
	return sha256.Sum256(b), nil
}
func (s *Store) external(ctx context.Context, tx *sql.Tx, issuer, subject, name string) (uuid.UUID, error) {
	k, e := identityKey(issuer, subject)
	if e != nil {
		return uuid.Nil, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_auth_external_guards")+"(identity_key) VALUES(?) ON DUPLICATE KEY UPDATE identity_key=VALUES(identity_key)", k[:]); e != nil {
		return uuid.Nil, e
	}
	var user mysqlstore.UUID
	var oldIssuer, oldSubject string
	e = tx.QueryRowContext(ctx, "SELECT user_id,issuer,subject FROM "+s.table("identity_external_identities")+" WHERE identity_key=?", k[:]).Scan(&user, &oldIssuer, &oldSubject)
	if e == nil {
		if issuer != oldIssuer || subject != oldSubject {
			return uuid.Nil, conflict
		}
		return uuid.UUID(user), nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return uuid.Nil, e
	}
	u := uuid.New()
	if len(name) == 0 || len(name) > 200 {
		return uuid.Nil, denied
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_users")+"(id,display_name,active) VALUES(?,?,TRUE)", id(u), name); e != nil {
		return uuid.Nil, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_external_identities")+"(identity_key,issuer,subject,user_id) VALUES(?,?,?,?)", k[:], issuer, subject, id(u))
	return u, e
}

// RegisterTenant is an explicit operator operation, never called by an HTTP login.
func (s *Store) RegisterTenant(ctx context.Context, tenant uuid.UUID) error {
	if tenant == uuid.Nil {
		return denied
	}
	_, e := s.DB.ExecContext(ctx, "INSERT INTO "+s.table("identity_tenants")+"(id,active) VALUES(?,TRUE) ON DUPLICATE KEY UPDATE id=VALUES(id)", id(tenant))
	return e
}
func (s *Store) Provision(ctx context.Context, tenant uuid.UUID, issuer, subject, name string, roles []string) (uuid.UUID, error) {
	if tenant == uuid.Nil || !rolesValid(roles) || subject != strings.TrimSpace(subject) {
		return uuid.Nil, denied
	}
	var user uuid.UUID
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		if e := s.lockAdmin(ctx, tx, tenant); e != nil {
			return e
		}
		var active bool
		if e := tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_tenants")+" WHERE id=? FOR SHARE", id(tenant)).Scan(&active); e != nil {
			return noRow(e)
		}
		if !active {
			return denied
		}
		u, e := s.external(ctx, tx, issuer, subject, name)
		if e != nil {
			return e
		}
		user = u
		var old string
		var enabled bool
		e = tx.QueryRowContext(ctx, "SELECT roles,active FROM "+s.table("identity_memberships")+" WHERE tenant_id=? AND user_id=? FOR UPDATE", id(tenant), id(user)).Scan(&old, &enabled)
		if e == nil {
			a, b := slices.Clone(roles), rolesList(old)
			slices.Sort(a)
			slices.Sort(b)
			if !enabled || !slices.Equal(a, b) {
				return conflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_memberships")+"(tenant_id,user_id,roles,active,version) VALUES(?,?,?,TRUE,1)", id(tenant), id(user), strings.Join(roles, ",")); e != nil {
			return e
		}
		return s.audit(ctx, tx, tenant, "operator-cli", "membership.provisioned", user, map[string]any{"roles": roles})
	})
	return user, e
}
func newLogin() (platform.Login, error) {
	var l platform.Login
	for _, dest := range []*string{&l.State, &l.Binding, &l.Nonce, &l.Verifier} {
		v, e := secret()
		if e != nil {
			return l, e
		}
		*dest = v
	}
	return l, nil
}
func (s *Store) begin(ctx context.Context, issuer string, invitation []byte) (platform.Login, error) {
	l, e := newLogin()
	if e != nil {
		return l, e
	}
	if issuer == "" || len(issuer) > 2048 {
		return l, denied
	}
	_, e = s.DB.ExecContext(ctx, "INSERT INTO "+s.table("identity_login_transactions")+"(digest,binding_digest,issuer,nonce,verifier,invitation_digest,expires_at) VALUES(?,?,?,?,?,?,TIMESTAMPADD(MINUTE,5,SYSDATE(6)))", sessioncredential.Digest(l.State), sessioncredential.Digest(l.Binding), issuer, l.Nonce, l.Verifier, invitation)
	return l, e
}
func (s *Store) BeginLogin(ctx context.Context, issuer string) (platform.Login, error) {
	return s.begin(ctx, issuer, nil)
}
func (s *Store) ConsumeLogin(ctx context.Context, state, binding, issuer string) (platform.Login, error) {
	if !sessioncredential.Valid(state) || !sessioncredential.Valid(binding) {
		return platform.Login{}, denied
	}
	var l platform.Login
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, "SELECT nonce,verifier,invitation_digest FROM "+s.table("identity_login_transactions")+" WHERE digest=? AND binding_digest=? AND issuer=? AND expires_at>SYSDATE(6) FOR UPDATE", sessioncredential.Digest(state), sessioncredential.Digest(binding), issuer).Scan(&l.Nonce, &l.Verifier, &l.InvitationDigest)
		if e != nil {
			return noRow(e)
		}
		var valid bool
		if e = tx.QueryRowContext(ctx, "SELECT expires_at>SYSDATE(6) FROM "+s.table("identity_login_transactions")+" WHERE digest=?", sessioncredential.Digest(state)).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return denied
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM "+s.table("identity_login_transactions")+" WHERE digest=?", sessioncredential.Digest(state))
		return e
	})
	return l, e
}
func (s *Store) insertSession(ctx context.Context, tx *sql.Tx, user, tenant uuid.UUID) (string, uuid.UUID, error) {
	raw, e := secret()
	sid := uuid.New()
	if e != nil {
		return "", sid, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_sessions")+"(id,digest,user_id,tenant_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,SYSDATE(6),TIMESTAMPADD(HOUR,8,SYSDATE(6)),TIMESTAMPADD(MINUTE,30,SYSDATE(6)))", id(sid), sessioncredential.Digest(raw), id(user), id(tenant))
	return raw, sid, e
}
func (s *Store) CreateSessionForTenant(ctx context.Context, issuer, subject string, tenant uuid.UUID) (string, error) {
	k, e := identityKey(issuer, subject)
	if e != nil {
		return "", e
	}
	var raw string
	e = s.transaction(ctx, func(tx *sql.Tx) error {
		var user mysqlstore.UUID
		var foundIssuer, foundSubject string
		e := tx.QueryRowContext(ctx, "SELECT user_id,issuer,subject FROM "+s.table("identity_external_identities")+" WHERE identity_key=?", k[:]).Scan(&user, &foundIssuer, &foundSubject)
		if e != nil {
			return noRow(e)
		}
		if foundIssuer != issuer || foundSubject != subject {
			return denied
		}
		var active bool
		if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_users")+" WHERE id=? FOR SHARE", user).Scan(&active); e != nil {
			return noRow(e)
		}
		if !active {
			return denied
		}
		var selected mysqlstore.UUID
		if tenant == uuid.Nil {
			e = tx.QueryRowContext(ctx, "SELECT m.tenant_id FROM "+s.table("identity_memberships")+" m JOIN "+s.table("identity_tenants")+" t ON t.id=m.tenant_id WHERE m.user_id=? AND m.active AND t.active ORDER BY m.tenant_id LIMIT 1 FOR SHARE", user).Scan(&selected)
		} else {
			selected = id(tenant)
			e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_memberships")+" WHERE tenant_id=? AND user_id=? FOR SHARE", selected, user).Scan(&active)
			if e == nil && !active {
				return denied
			}
		}
		if e != nil {
			return noRow(e)
		}
		if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_tenants")+" WHERE id=? FOR SHARE", selected).Scan(&active); e != nil {
			return noRow(e)
		}
		if !active {
			return denied
		}
		var sid uuid.UUID
		raw, sid, e = s.insertSession(ctx, tx, uuid.UUID(user), uuid.UUID(selected))
		if e != nil {
			return e
		}
		return s.audit(ctx, tx, uuid.UUID(selected), uuid.UUID(user).String(), "session.created", sid, struct{}{})
	})
	return raw, e
}
func (s *Store) Resolve(ctx context.Context, raw string) (platform.Session, error) {
	var v platform.Session
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		var e error
		v, e = s.lockCaller(ctx, tx, raw, true, false, true)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_sessions")+" SET idle_until=LEAST(expires_at,TIMESTAMPADD(MINUTE,30,SYSDATE(6))) WHERE id=? AND idle_until<TIMESTAMPADD(MINUTE,25,SYSDATE(6))", id(v.ID))
		return e
	})
	return v, e
}
func (s *Store) Logout(ctx context.Context, raw string) error {
	if !sessioncredential.Valid(raw) {
		return nil
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		// Possession of the opaque credential is sufficient to revoke it, even
		// after suspension/expiry. Logout must remain final after reactivation.
		var sid, user, tenant mysqlstore.UUID
		var revoked sql.NullTime
		e := tx.QueryRowContext(ctx, "SELECT id,user_id,tenant_id,revoked_at FROM "+s.table("identity_sessions")+" WHERE digest=? FOR UPDATE", sessioncredential.Digest(raw)).Scan(&sid, &user, &tenant, &revoked)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if revoked.Valid {
			return nil
		}
		if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_sessions")+" SET revoked_at=SYSDATE(6) WHERE id=?", sid); e != nil {
			return e
		}
		return s.audit(ctx, tx, uuid.UUID(tenant), uuid.UUID(user).String(), "session.revoked", uuid.UUID(sid), struct{}{})
	})
}
func (s *Store) Sessions(ctx context.Context, raw string, after uuid.UUID) ([]platform.SessionInfo, error) {
	out := []platform.SessionInfo{}
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		v, e := s.caller(ctx, tx, raw, false, false)
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, "SELECT id,created_at,expires_at,idle_until,id=? FROM "+s.table("identity_sessions")+" WHERE tenant_id=? AND user_id=? AND id>? AND revoked_at IS NULL AND expires_at>SYSDATE(6) AND idle_until>SYSDATE(6) ORDER BY id LIMIT 101", id(v.ID), id(v.TenantID), id(v.UserID), id(after))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var item platform.SessionInfo
			var sid mysqlstore.UUID
			if e = rows.Scan(&sid, &item.CreatedAt, &item.ExpiresAt, &item.IdleUntil, &item.Current); e != nil {
				return e
			}
			item.ID = uuid.UUID(sid)
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) RevokeSessions(ctx context.Context, raw string, target uuid.UUID, all bool) (bool, error) {
	if (all && target != uuid.Nil) || (!all && target == uuid.Nil) {
		return false, denied
	}
	current := false
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		v, e := s.caller(ctx, tx, raw, true, false)
		if e != nil {
			return e
		}
		if !all {
			var owned bool
			if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.table("identity_sessions")+" WHERE tenant_id=? AND user_id=? AND id=?)", id(v.TenantID), id(v.UserID), id(target)).Scan(&owned); e != nil {
				return e
			}
			if !owned {
				return denied
			}
		}
		r, e := tx.ExecContext(ctx, "UPDATE "+s.table("identity_sessions")+" SET revoked_at=SYSDATE(6) WHERE tenant_id=? AND user_id=? AND revoked_at IS NULL AND (? OR id=?) ORDER BY id", id(v.TenantID), id(v.UserID), all, id(target))
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		current = all || target == v.ID
		if n == 0 {
			return nil
		}
		action := "session.revoked"
		if all {
			action = "sessions.revoked_all"
			target = v.ID
		}
		return s.audit(ctx, tx, v.TenantID, v.UserID.String(), action, target, map[string]any{"count": n})
	})
	return current, e
}
func (s *Store) Members(ctx context.Context, raw string, after uuid.UUID) ([]platform.Member, error) {
	out := []platform.Member{}
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		c, e := s.caller(ctx, tx, raw, false, true)
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, "SELECT u.id,u.display_name,m.roles,m.active AND u.active,m.version FROM "+s.table("identity_memberships")+" m JOIN "+s.table("identity_users")+" u ON u.id=m.user_id WHERE m.tenant_id=? AND m.user_id>? ORDER BY m.user_id LIMIT 101", id(c.TenantID), id(after))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v platform.Member
			var u mysqlstore.UUID
			var roles string
			if e = rows.Scan(&u, &v.Name, &roles, &v.Active, &v.Version); e != nil {
				return e
			}
			v.UserID = uuid.UUID(u)
			v.Roles = rolesList(roles)
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) ChangeMember(ctx context.Context, raw string, target uuid.UUID, version int64, roles []string, active bool) error {
	if target == uuid.Nil || version < 1 || !rolesValid(roles) {
		return denied
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		c, e := s.caller(ctx, tx, raw, true, true)
		if e != nil {
			return e
		}
		var current string
		var enabled bool
		var v int64
		if e = tx.QueryRowContext(ctx, "SELECT roles,active,version FROM "+s.table("identity_memberships")+" WHERE tenant_id=? AND user_id=? FOR UPDATE", id(c.TenantID), id(target)).Scan(&current, &enabled, &v); e != nil {
			return noRow(e)
		}
		if v != version {
			return conflict
		}
		if enabled && slices.Contains(rolesList(current), "tenant_admin") && (!active || !slices.Contains(roles, "tenant_admin")) {
			var n int
			if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT m.user_id FROM "+s.table("identity_memberships")+" m JOIN "+s.table("identity_users")+" u ON u.id=m.user_id WHERE m.tenant_id=? AND m.active AND u.active AND FIND_IN_SET('tenant_admin',m.roles)>0 LIMIT 2) admins", id(c.TenantID)).Scan(&n); e != nil {
				return e
			}
			if n <= 1 {
				return conflict
			}
		}
		// A target lock wait may consume the caller's remaining session lifetime.
		if _, e = s.session(ctx, tx, raw); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_memberships")+" SET roles=?,active=?,version=version+1 WHERE tenant_id=? AND user_id=?", strings.Join(roles, ","), active, id(c.TenantID), id(target)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_sessions")+" SET revoked_at=SYSDATE(6) WHERE tenant_id=? AND user_id=? AND revoked_at IS NULL ORDER BY id", id(c.TenantID), id(target)); e != nil {
			return e
		}
		return s.audit(ctx, tx, c.TenantID, c.UserID.String(), "membership.changed", target, map[string]any{"roles": roles, "active": active, "previous_roles": rolesList(current), "previous_active": enabled})
	})
}
func (s *Store) Audit(ctx context.Context, raw string, after uuid.UUID) ([]platform.AuditEvent, error) {
	out := []platform.AuditEvent{}
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		c, e := s.caller(ctx, tx, raw, false, true)
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, "SELECT id,actor_id,action,target_id,created_at,details FROM "+s.table("identity_audit_events")+" WHERE tenant_id=? AND id>? ORDER BY id LIMIT 101", id(c.TenantID), id(after))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v platform.AuditEvent
			var u, t mysqlstore.UUID
			var at time.Time
			if e = rows.Scan(&u, &v.Actor, &v.Action, &t, &at, &v.Details); e != nil {
				return e
			}
			v.ID, v.Target, v.At = uuid.UUID(u), uuid.UUID(t), at.UTC().Format(time.RFC3339Nano)
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
