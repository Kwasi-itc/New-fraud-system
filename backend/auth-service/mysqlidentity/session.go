package mysqlidentity

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrDenied = errors.New("identity session denied")

// Session contains verified identities only, never a secret or digest. SQL
// authorization must still recheck current versions/expiry on each transaction.
type Session struct {
	ActorID, TenantID, SessionID uuid.UUID
	MembershipVersion            int64
	ReadOnly                     bool
}
type Verifier struct {
	db               *sql.DB
	database, schema string
}

func NewVerifier(db *sql.DB, database string) (*Verifier, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	if db == nil {
		return nil, errors.New("identity pool required")
	}
	return &Verifier{db, database, s}, nil
}
func (v *Verifier) Matches(db *sql.DB, database string) bool {
	return v != nil && v.db == db && v.database == database
}

// Resolve uses the established 32-byte opaque session secret and SHA-256 digest
// index. Route IDs, role headers and signed-but-unverified claims are not inputs.
// It deliberately does not renew idle expiry; the auth owner controls renewal.
func (v *Verifier) Resolve(ctx context.Context, raw string) (Session, error) {
	if v == nil || !sessioncredential.Valid(raw) {
		return Session{}, ErrDenied
	}
	var actor, tenant, sid mysqlstore.UUID
	var version int64
	var ro bool
	err := v.db.QueryRowContext(ctx, "SELECT s.user_id,s.tenant_id,s.id,m.version,s.read_only FROM "+v.schema+`.identity_sessions s
 JOIN `+v.schema+`.identity_users u ON u.id=s.user_id
 JOIN `+v.schema+`.identity_memberships m ON m.tenant_id=s.tenant_id AND m.user_id=s.user_id
 JOIN `+v.schema+`.identity_tenants t ON t.id=s.tenant_id
 WHERE s.digest=? AND s.revoked_at IS NULL AND s.expires_at>SYSDATE(6) AND s.idle_until>SYSDATE(6)
 AND u.active=TRUE AND m.active=TRUE AND t.active=TRUE`, sessioncredential.Digest(raw)).Scan(&actor, &tenant, &sid, &version, &ro)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrDenied
	}
	if err != nil {
		return Session{}, err
	}
	return Session{uuid.UUID(actor), uuid.UUID(tenant), uuid.UUID(sid), version, ro}, nil
}
