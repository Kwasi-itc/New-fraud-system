package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrAdditionalScoringDeferred = errors.New("additional scoring is deferred: configured or unfinished work requires its provider")

// WithoutAdditionalScoring returns a detached capability. Rejected request
// creation poisons the enclosing transaction, including prepared executions.
// Internal rule scores and retained request history are unaffected.
func (s *EffectStore) WithoutAdditionalScoring() (*EffectStore, error) {
	if s == nil || s.configuration == nil {
		return nil, ErrOwnerContractUnavailable
	}
	copy := *s
	copy.deferScoring = true
	return &copy, nil
}

// CheckAdditionalScoringDeferred is a cold-start gate, not a work consumer.
// It never disables configuration, changes requests or deletes queued work.
// Run after the release write fence has stopped the previous runtime's writers.
func (s *EffectStore) CheckAdditionalScoringDeferred(ctx context.Context) error {
	if s == nil || s.configuration == nil || !s.deferScoring {
		return ErrOwnerContractUnavailable
	}
	c := s.configuration
	return mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		for _, query := range []string{
			"SELECT EXISTS(SELECT 1 FROM " + c.schema + ".decision_engine_scoring_configs WHERE tenant_id=? AND active=1)",
			"SELECT EXISTS(SELECT 1 FROM " + c.schema + ".decision_engine_scoring_requests WHERE tenant_id=? AND status IN ('pending','queued','sent','failed'))",
			"SELECT EXISTS(SELECT 1 FROM " + c.schema + ".decision_engine_effect_job_deliveries WHERE tenant_id=? AND kind='scoring_request' AND state IN ('available','running'))",
		} {
			var present bool
			if err := tx.QueryRowContext(ctx, query, mysqlstore.UUID(c.tenant)).Scan(&present); err != nil {
				return err
			}
			if present {
				return ErrAdditionalScoringDeferred
			}
		}
		return nil
	})
}
