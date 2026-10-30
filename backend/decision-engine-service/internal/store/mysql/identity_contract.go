package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrDecisionAuthorizationDenied = errors.New("decision authorization denied")
var ErrDecisionAuthorizationUnavailable = errors.New("decision authorization contract unavailable")

// TrustedDecisionSession is supplied only by the established credential verifier,
// never decoded directly from an HTTP body/header's unverified role claims.
// Auth owns role-to-action policy; decision must not invent grants or defaults.
type TrustedDecisionSession struct {
	ActorID, TenantID, SessionID uuid.UUID
	MembershipVersion            int64
	ReadOnly                     bool
}

// SessionAuthorizer is the consumer side of the approved auth-owned MySQL
// contract. The provider is NOT implemented here. It must check current active
// user/tenant/membership, version, session binding/revocation/current expiry and
// requested action. For writes it locks user -> membership -> session before
// owner locks, in this transaction; no COMMIT, DDL or external calls. Missing,
// NULL or invalid provider results fail closed as unavailable, never allow.
func SessionAuthorizer(database string, session TrustedDecisionSession) (AuthoringAuthorizer, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if session.ActorID == uuid.Nil || session.TenantID == uuid.Nil || session.SessionID == uuid.Nil || session.MembershipVersion <= 0 {
		return nil, ErrDecisionAuthorizationDenied
	}
	return func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, action AuthoringAction) error {
		if tx == nil {
			return ErrDecisionAuthorizationUnavailable
		}
		if tenant != session.TenantID {
			return ErrDecisionAuthorizationDenied
		}
		write := action != AuthoringRead && action != AuthoringWorkflowRead && action != AuthoringCustomListRead
		if action != AuthoringRead && action != AuthoringWrite && action != AuthoringPublish && action != AuthoringWorkflowRead && action != AuthoringWorkflowWrite && action != AuthoringCustomListRead && action != AuthoringCustomListWrite {
			return ErrDecisionAuthorizationDenied
		}
		if write && session.ReadOnly {
			return ErrDecisionAuthorizationDenied
		}
		var allowed mysqlstore.Bool
		err := tx.QueryRowContext(ctx, "SELECT "+schema+".identity_authorize_decision_v1(?,?,?,?,?,?)", mysqlstore.UUID(session.ActorID), mysqlstore.UUID(tenant), mysqlstore.UUID(session.SessionID), session.MembershipVersion, mysqlstore.Text(string(action)), write).Scan(&allowed)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrDecisionAuthorizationUnavailable, err)
		}
		if !allowed {
			return ErrDecisionAuthorizationDenied
		}
		return nil
	}, nil
}
