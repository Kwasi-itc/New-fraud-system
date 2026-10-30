package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// TagContract consumes the case owner's approved bounded authorization function.
// The provider, not decision code, owns permission/active-tag rules. It validates
// trusted actor/session/membership and all requested same-tenant tag identities,
// with deterministic UUID lock order when locks=true. Missing/NULL contracts
// fail closed; there are no case-table writes, external calls or default roles.
// Compose the auth-owned check before inbox/tag/domain locks on the SAME tx.
type TagContract struct {
	schema  string
	session TrustedDecisionSession
}

func NewTagContract(database string, session TrustedDecisionSession) (TagContract, error) {
	if _, err := SessionAuthorizer(database, session); err != nil {
		return TagContract{}, err
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return TagContract{}, err
	}
	return TagContract{schema, session}, nil
}
func (c TagContract) Authorize(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, tags []uuid.UUID, write, requireActive bool) error {
	if tx == nil || c.schema == "" {
		return ErrOwnerContractUnavailable
	}
	if tenant != c.session.TenantID || write && c.session.ReadOnly {
		return ErrDecisionAuthorizationDenied
	}
	if len(tags) > 25 {
		return ErrProjectionLimit
	}
	ordered := append([]uuid.UUID(nil), tags...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	ids := make([]string, len(ordered))
	for i, id := range ordered {
		if id == uuid.Nil || i > 0 && ordered[i-1] == id {
			return ErrTenantScope
		}
		ids[i] = id.String()
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	var allowed mysqlstore.Bool
	query := "SELECT " + c.schema + ".case_manager_authorize_decision_tags_v1(?,?,?,?,?,?,?)"
	if err := tx.QueryRowContext(ctx, query, mysqlstore.UUID(c.session.ActorID), mysqlstore.UUID(tenant), mysqlstore.UUID(c.session.SessionID), c.session.MembershipVersion, mysqlstore.JSON(raw), requireActive, write).Scan(&allowed); err != nil {
		return errors.Join(ErrOwnerContractUnavailable, err)
	}
	if !allowed {
		return ErrDecisionAuthorizationDenied
	}
	return nil
}
