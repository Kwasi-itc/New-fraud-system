package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

//go:embed schema_v2.sql
var schemaV2 string

//go:embed schema_v3.sql
var schemaV3 string

const SchemaVersion = 3

var ErrDenied = errors.New("identity or permission denied")
var ErrConflict = errors.New("account changed or last active tenant administrator would be removed")

type Store struct{ DB *pgxpool.Pool }

func (s Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='30s'; SELECT pg_advisory_xact_lock(8163072026)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schema); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schemaV2); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schemaV3); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(v string) []byte { h := sha256.Sum256([]byte(v)); return h[:] }
func validSecret(v string) bool {
	b, e := base64.RawURLEncoding.DecodeString(v)
	return e == nil && len(b) == 32
}
func validRoles(roles []string) bool {
	if len(roles) == 0 || len(roles) > 7 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if seen[r] || !slices.Contains([]string{"tenant_admin", "case_manager_admin", "case_investigator", "platform_viewer", "platform_builder", "platform_publisher", "platform_admin"}, r) {
			return false
		}
		seen[r] = true
	}
	return true
}
func audit(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, actor, action string, target uuid.UUID, details any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity.audit_events(id,tenant_id,actor_id,action,target_id,details) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), tenant, actor, action, target, data)
	return err
}

// Provision is an explicit operator command, never a public registration API.
// It does not auto-link email or overwrite an existing membership's permissions.
func (s Store) Provision(ctx context.Context, tenant uuid.UUID, issuer, subject, name string, roles []string) (uuid.UUID, error) {
	if tenant == uuid.Nil || issuer == "" || strings.TrimSpace(subject) == "" || len(subject) > 1024 || subject != strings.TrimSpace(subject) || len(name) == 0 || len(name) > 200 || !validRoles(roles) {
		return uuid.Nil, ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, issuer+"\n"+subject); err != nil {
		return uuid.Nil, err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT status='active' FROM core.tenants WHERE id=$1`, tenant).Scan(&active); err != nil || !active {
		return uuid.Nil, ErrDenied
	}
	var user uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM identity.external_identities WHERE issuer=$1 AND subject=$2`, issuer, subject).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		user = uuid.New()
		if _, err = tx.Exec(ctx, `INSERT INTO identity.users(id,display_name) VALUES($1,$2)`, user, name); err != nil {
			return uuid.Nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO identity.external_identities(issuer,subject,user_id) VALUES($1,$2,$3)`, issuer, subject, user); err != nil {
			return uuid.Nil, err
		}
	} else if err != nil {
		return uuid.Nil, err
	}
	result, err := tx.Exec(ctx, `INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenant, user, roles)
	if err != nil {
		return uuid.Nil, err
	}
	if result.RowsAffected() == 0 {
		var existing []string
		var enabled bool
		if err = tx.QueryRow(ctx, `SELECT roles,active FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2`, tenant, user).Scan(&existing, &enabled); err != nil {
			return uuid.Nil, err
		}
		a, b := slices.Clone(existing), slices.Clone(roles)
		slices.Sort(a)
		slices.Sort(b)
		if !enabled || !slices.Equal(a, b) {
			return uuid.Nil, ErrConflict
		}
	} else if err = audit(ctx, tx, tenant, "operator-cli", "membership.provisioned", user, map[string]any{"roles": roles}); err != nil {
		return uuid.Nil, err
	}
	return user, tx.Commit(ctx)
}

type Login struct {
	State            string `json:"state"`
	Binding          string `json:"binding"`
	Nonce            string `json:"-"`
	Verifier         string `json:"-"`
	InvitationDigest []byte `json:"-"`
}

func (s Store) BeginLogin(ctx context.Context, issuer string) (Login, error) {
	l := Login{State: secret(), Binding: secret(), Nonce: secret(), Verifier: secret()}
	_, err := s.DB.Exec(ctx, `INSERT INTO identity.login_transactions(digest,binding_digest,issuer,nonce,verifier,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '5 minutes')`, digest(l.State), digest(l.Binding), issuer, l.Nonce, l.Verifier)
	return l, err
}

// DELETE RETURNING consumes the attempt before any provider network call.
func (s Store) ConsumeLogin(ctx context.Context, state, binding, issuer string) (Login, error) {
	if !validSecret(state) || !validSecret(binding) {
		return Login{}, ErrDenied
	}
	var l Login
	err := s.DB.QueryRow(ctx, `DELETE FROM identity.login_transactions WHERE digest=$1 AND binding_digest=$2 AND issuer=$3 AND expires_at>now() RETURNING nonce,verifier,invitation_digest`, digest(state), digest(binding), issuer).Scan(&l.Nonce, &l.Verifier, &l.InvitationDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return Login{}, ErrDenied
	}
	return l, err
}

type Session struct {
	ID       uuid.UUID `json:"id"`
	UserID   uuid.UUID `json:"user_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	Name     string    `json:"name"`
	Roles    []string  `json:"roles"`
	Version  int64     `json:"version"`
	Expires  time.Time `json:"expires_at"`
}

func session(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, raw string) (Session, error) {
	if !validSecret(raw) {
		return Session{}, ErrDenied
	}
	var s Session
	err := q.QueryRow(ctx, `SELECT s.id,s.user_id,s.tenant_id,u.display_name,m.roles,m.version,s.expires_at
 FROM identity.sessions s JOIN identity.users u ON u.id=s.user_id JOIN identity.memberships m ON m.user_id=s.user_id AND m.tenant_id=s.tenant_id
 JOIN core.tenants t ON t.id=s.tenant_id WHERE s.digest=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND s.idle_until>now() AND u.active AND m.active AND t.status='active'`, digest(raw)).Scan(&s.ID, &s.UserID, &s.TenantID, &s.Name, &s.Roles, &s.Version, &s.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrDenied
	}
	return s, err
}
func (s Store) Resolve(ctx context.Context, raw string) (Session, error) {
	v, err := session(ctx, s.DB, raw)
	if err != nil {
		return v, err
	}
	_, err = s.DB.Exec(ctx, `UPDATE identity.sessions SET idle_until=LEAST(expires_at,now()+interval '30 minutes') WHERE id=$1 AND revoked_at IS NULL AND idle_until>now() AND idle_until<now()+interval '25 minutes'`, v.ID)
	return v, err
}
func (s Store) CreateSession(ctx context.Context, issuer, subject string) (string, error) {
	return s.CreateSessionForTenant(ctx, issuer, subject, uuid.Nil)
}

// CreateSessionForTenant selects a configured login tenant without granting access.
// A nonzero tenant requires an active membership; it never falls back to another tenant.
// Existing sessions and invitation-selected tenants are unaffected.
func (s Store) CreateSessionForTenant(ctx context.Context, issuer, subject string, requestedTenant uuid.UUID) (string, error) {
	raw := secret()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	var user, tenant uuid.UUID
	err = tx.QueryRow(ctx, `SELECT u.id FROM identity.external_identities e JOIN identity.users u ON u.id=e.user_id WHERE e.issuer=$1 AND e.subject=$2 AND u.active FOR SHARE OF u`, issuer, subject).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDenied
	}
	if err != nil {
		return "", err
	}
	if requestedTenant == uuid.Nil {
		// Preserve legacy selection only when no login tenant is configured.
		err = tx.QueryRow(ctx, `SELECT m.tenant_id FROM identity.memberships m JOIN core.tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 AND m.active AND t.status='active' ORDER BY m.tenant_id LIMIT 1 FOR SHARE OF m`, user).Scan(&tenant)
	} else {
		err = tx.QueryRow(ctx, `SELECT m.tenant_id FROM identity.memberships m JOIN core.tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 AND m.tenant_id=$2 AND m.active AND t.status='active' FOR SHARE OF m`, user, requestedTenant).Scan(&tenant)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDenied
	}
	if err != nil {
		return "", err
	}
	id := uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO identity.sessions(id,digest,user_id,tenant_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '8 hours',now()+interval '30 minutes')`, id, digest(raw), user, tenant); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, tenant, user.String(), "session.created", id, struct{}{}); err != nil {
		return "", err
	}
	return raw, tx.Commit(ctx)
}
func (s Store) Logout(ctx context.Context, raw string) error {
	if !validSecret(raw) {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var id, user, tenant uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE identity.sessions SET revoked_at=now() WHERE digest=$1 AND revoked_at IS NULL RETURNING id,user_id,tenant_id`, digest(raw)).Scan(&id, &user, &tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, tenant, user.String(), "session.revoked", id, struct{}{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Member struct {
	UserID  uuid.UUID `json:"user_id"`
	Name    string    `json:"name"`
	Roles   []string  `json:"roles"`
	Active  bool      `json:"active"`
	Version int64     `json:"version"`
}

func (s Store) Members(ctx context.Context, raw string, after uuid.UUID) ([]Member, error) {
	caller, err := s.Resolve(ctx, raw)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(caller.Roles, "tenant_admin") {
		return nil, ErrDenied
	}
	rows, err := s.DB.Query(ctx, `SELECT u.id,u.display_name,m.roles,m.active AND u.active,m.version FROM identity.memberships m JOIN identity.users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND u.id>$2 ORDER BY u.id LIMIT 101`, caller.TenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.UserID, &m.Name, &m.Roles, &m.Active, &m.Version); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s Store) ChangeMember(ctx context.Context, raw string, target uuid.UUID, version int64, roles []string, active bool) error {
	if target == uuid.Nil || version < 1 || !validRoles(roles) {
		return ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	caller, err := session(ctx, tx, raw)
	if err != nil {
		return err
	}
	// Only administrative membership changes serialize per tenant; case writes do not.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "identity-admin:"+caller.TenantID.String()); err != nil {
		return err
	}
	caller, err = session(ctx, tx, raw)
	if err != nil {
		return err
	}
	if !slices.Contains(caller.Roles, "tenant_admin") {
		return ErrDenied
	}
	var enabled bool
	var current []string
	var currentVersion int64
	err = tx.QueryRow(ctx, `SELECT active,roles,version FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE`, caller.TenantID, target).Scan(&enabled, &current, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if currentVersion != version {
		return ErrConflict
	}
	var revoked bool
	if err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL OR expires_at<=now() OR idle_until<=now() FROM identity.sessions WHERE id=$1 FOR SHARE`, caller.ID).Scan(&revoked); err != nil {
		return err
	}
	if revoked {
		return ErrDenied
	}
	if enabled && slices.Contains(current, "tenant_admin") && (!active || !slices.Contains(roles, "tenant_admin")) {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM identity.memberships m JOIN identity.users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.active AND u.active AND 'tenant_admin'=ANY(m.roles) LIMIT 2) admins`, caller.TenantID).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return ErrConflict
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.memberships SET roles=$3,active=$4,version=version+1 WHERE tenant_id=$1 AND user_id=$2`, caller.TenantID, target, roles, active); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.sessions SET revoked_at=now() WHERE tenant_id=$1 AND user_id=$2 AND revoked_at IS NULL`, caller.TenantID, target); err != nil {
		return err
	}
	if err = audit(ctx, tx, caller.TenantID, caller.UserID.String(), "membership.changed", target, map[string]any{"roles": roles, "active": active, "previous_roles": current, "previous_active": enabled}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
