package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// DecisionStore is a staged storage boundary, NOT the live evaluation
// TransactionManager: workflow effects and required jobs must be composed before
// runtime may select it. The required authorizer must grant decision persistence
// or history access, not infer that permission from a supplied tenant ID.
type DecisionStore struct{ configuration *PlatformStore }
type DecisionRepositories struct {
	Decisions             DecisionRepository
	RuleExecutions        RuleExecutionRepository
	TestRuns              TestRunRepository
	PhantomDecisions      PhantomDecisionRepository
	PhantomRuleExecutions PhantomRuleExecutionRepository
	RuleSnoozes           RuleSnoozeRepository
}

func NewDecisionStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize PlatformAuthorizer) (*DecisionStore, error) {
	c, err := NewPlatformStore(db, database, tenant, maxRows, maxBytes, authorize)
	if err != nil {
		return nil, err
	}
	return &DecisionStore{c}, nil
}
func (s *DecisionStore) Mutate(ctx context.Context, fn func(DecisionRepositories) error) error {
	return s.run(ctx, true, fn)
}
func (s *DecisionStore) ReadSnapshot(ctx context.Context, fn func(DecisionRepositories) error) error {
	return s.run(ctx, false, fn)
}
func (s *DecisionStore) run(ctx context.Context, write bool, fn func(DecisionRepositories) error) error {
	if s == nil || s.configuration == nil || fn == nil {
		return errors.New("decision store and callback required")
	}
	c := s.configuration
	run := mysqlstore.Snapshot
	if write {
		run = mysqlstore.Mutation
	}
	return run(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, write); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, write}}
		return fn(DecisionRepositories{Decisions: DecisionRepository{r}, RuleExecutions: RuleExecutionRepository{r}, TestRuns: TestRunRepository{r}, PhantomDecisions: PhantomDecisionRepository{r}, PhantomRuleExecutions: PhantomRuleExecutionRepository{r}, RuleSnoozes: RuleSnoozeRepository{r}})
	})
}
