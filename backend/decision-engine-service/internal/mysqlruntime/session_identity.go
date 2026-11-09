package mysqlruntime

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

type verifiedSessionKey struct{}
type verifiedSession struct {
	verifier *mysqlidentity.Verifier
	value    mysql.TrustedDecisionSession
}

// BindSessionIdentity installs the real auth-owner opaque-credential verifier.
// This staged bearer boundary accepts an actual session secret, never actor or
// role headers. The browser gateway remains responsible for cookie/CSRF policy.
// Service authority stays separately configured; no user role implies worker,
// execution mutation or generic persistence authority.
func BindSessionIdentity(deps Dependencies, verifier *mysqlidentity.Verifier) (Dependencies, error) {
	if !verifier.Matches(deps.DB, deps.Database) {
		return Dependencies{}, mysql.ErrDecisionAuthorizationUnavailable
	}
	deps.Authenticate = func(r *http.Request) (context.Context, error) {
		if r == nil {
			return nil, mysql.ErrDecisionAuthorizationDenied
		}
		header := r.Header.Values("Authorization")
		if len(header) != 1 || !strings.HasPrefix(header[0], "Bearer ") {
			return nil, mysql.ErrDecisionAuthorizationDenied
		}
		raw := strings.TrimPrefix(header[0], "Bearer ")
		if strings.TrimSpace(raw) != raw {
			return nil, mysql.ErrDecisionAuthorizationDenied
		}
		session, err := verifier.Resolve(r.Context(), raw)
		if err != nil {
			return nil, err
		}
		trusted := mysql.TrustedDecisionSession{ActorID: session.ActorID, TenantID: session.TenantID, SessionID: session.SessionID, MembershipVersion: session.MembershipVersion, ReadOnly: session.ReadOnly}
		return context.WithValue(r.Context(), verifiedSessionKey{}, verifiedSession{verifier, trusted}), nil
	}
	deps.RequestAuthority = func(ctx context.Context, tenant uuid.UUID) (Authorities, error) {
		trusted, ok := ctx.Value(verifiedSessionKey{}).(verifiedSession)
		if !ok || trusted.verifier != verifier || trusted.value.TenantID != tenant {
			return Authorities{}, mysql.ErrDecisionAuthorizationDenied
		}
		session := trusted.value
		auth, err := mysql.SessionAuthorizer(deps.Database, session)
		if err != nil {
			return Authorities{}, err
		}
		reads := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
			if write {
				return mysql.ErrDecisionAuthorizationDenied
			}
			return auth(ctx, tx, tenant, mysql.AuthoringRead)
		}
		deny := func(context.Context, *sql.Tx, uuid.UUID, bool) error { return mysql.ErrDecisionAuthorizationDenied }
		execution := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, action mysql.ExecutionAction) error {
			if action != mysql.ExecutionRead {
				return mysql.ErrDecisionAuthorizationDenied
			}
			return auth(ctx, tx, tenant, mysql.AuthoringRead)
		}
		return Authorities{TenantID: tenant, Authoring: auth, Reads: reads, Persist: deny, Executions: execution, Session: &session}, nil
	}
	return deps, nil
}
