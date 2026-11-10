package mysqlruntime

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/google/uuid"
	"net/http"
)

// The local browser rollout accepts real opaque auth sessions. It keeps service
// worker authority separate and rechecks current membership on every SQL command.
// The existing staged production binder retains its read-only execution policy.
type presentedLocalCredential struct{}

// Preserve an explicitly presented credential through the development origin
// wrapper. Missing credentials retain the existing local operator behavior;
// presented sessions must reach the real verifier, including invalid sessions.
func localFrontendHandler(next http.Handler, enabled, origin, token string) (http.Handler, error) {
	if ec2test.Enabled() {
		return ec2test.RequirePresented(next, origin)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value, ok := r.Context().Value(presentedLocalCredential{}).(string); ok {
			r.Header.Set("Authorization", value)
		}
		next.ServeHTTP(w, r)
	})
	guarded, err := localdev.WithoutLogin(inner, enabled, origin, token)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) > 1 {
			http.Error(w, "invalid authorization", http.StatusUnauthorized)
			return
		}
		if len(values) == 1 {
			r = r.WithContext(context.WithValue(r.Context(), presentedLocalCredential{}, values[0]))
		}
		guarded.ServeHTTP(w, r)
	}), nil
}

func localFrontendIdentity(db *sql.DB) (Dependencies, error) {
	verifier, err := mysqlidentity.NewVerifier(db, "fraud")
	if err != nil {
		return Dependencies{}, err
	}
	deps, err := BindSessionIdentity(Dependencies{DB: db, Database: "fraud"}, verifier)
	if err != nil {
		return Dependencies{}, err
	}
	bound := deps.RequestAuthority
	deps.RequestAuthority = func(ctx context.Context, tenant uuid.UUID) (Authorities, error) {
		a, err := bound(ctx, tenant)
		if err != nil {
			return Authorities{}, err
		}
		auth := a.Authoring
		draft := func(ctx context.Context, tx *sql.Tx, t uuid.UUID, write bool) error {
			action := store.AuthoringRead
			if write {
				action = store.AuthoringWrite
			}
			return auth(ctx, tx, t, action)
		}
		a.Persist = draft
		a.PlatformMutations = draft
		a.EffectMutations = draft
		a.Executions = func(ctx context.Context, tx *sql.Tx, t uuid.UUID, action store.ExecutionAction) error {
			return draft(ctx, tx, t, action != store.ExecutionRead)
		}
		a.EffectRecovery = func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, store.EffectRetryAction) error {
			return store.ErrDecisionAuthorizationDenied
		}
		s := a.Session
		a.ReviewSession = &mysqlidentity.Session{ActorID: s.ActorID, TenantID: s.TenantID, SessionID: s.SessionID, MembershipVersion: s.MembershipVersion, ReadOnly: s.ReadOnly}
		return a, nil
	}
	return deps, nil
}
