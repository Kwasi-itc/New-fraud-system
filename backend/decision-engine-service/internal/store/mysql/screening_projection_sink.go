package mysql

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ScreeningProjectionSink exposes only the decision-owned reconciliation
// mutation to a same-pool screening projector. It grants no source mutations.
type ScreeningProjectionSink struct{ owner *EffectStore }

func NewScreeningProjectionSink(owner *EffectStore) (*ScreeningProjectionSink, error) {
	if owner == nil || owner.configuration == nil || owner.screening == nil {
		return nil, ErrOwnerContractUnavailable
	}
	return &ScreeningProjectionSink{owner}, nil
}
func (s *ScreeningProjectionSink) Matches(db *sql.DB, database string, tenant uuid.UUID) bool {
	if s == nil || s.owner == nil {
		return false
	}
	schema, err := mysqlstore.Identifier(database)
	c := s.owner.configuration
	return err == nil && c.db == db && c.schema == schema && c.tenant == tenant
}
func (s *ScreeningProjectionSink) Prepare(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) (func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error, error) {
	if s == nil || s.owner == nil || s.owner.configuration == nil || tx == nil || tenant != s.owner.configuration.tenant {
		return nil, ErrTenantScope
	}
	c := s.owner.configuration
	if err := c.authorize(ctx, tx, tenant, true); err != nil {
		return nil, err
	}
	return func(ctx context.Context, source, decision, config uuid.UUID) error {
		var failed error
		r := s.owner.repositories(tx, true, nil, &failed)
		_, err := r.ScreeningExecutions.ReconcileSource(ctx, tenant.String(), source.String(), decision.String(), config.String())
		return err
	}, nil
}
