package mysql

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// HistoryRepositories exposes only decision-owned records. Foreign screening,
// inbox and list mutation contracts are deliberately absent.
type HistoryRepositories struct {
	Scenarios          ScenarioRepository
	Decisions          DecisionRepository
	RuleExecutions     RuleExecutionRepository
	TestRuns           TestRunRepository
	RuleSnoozes        RuleSnoozeRepository
	WorkflowExecutions WorkflowExecutionRepository
	ScoringRequests    ScoringRequestRepository
	OutboxEvents       OutboxEventRepository
}

// RequireHistoryDecision checks ownership without loading the request payload.
func (r DecisionRepository) RequireHistoryDecision(ctx context.Context, tenant, id string) error {
	args, err := r.ids(tenant, id)
	if err != nil {
		return err
	}
	var found mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_decisions WHERE tenant_id=? AND id=?", args...).Scan(&found)
}

func (s *DecisionStore) MatchesExecutionStore(e *ExecutionStore) bool {
	if s == nil || s.configuration == nil || e == nil || e.configuration == nil {
		return false
	}
	a, b := s.configuration, e.configuration
	return a.db == b.db && a.schema == b.schema && a.tenant == b.tenant && a.maxRows == b.maxRows && a.maxBytes == b.maxBytes
}

// ReadHistory authorizes every request inside a single read-only snapshot.
// Repositories cannot be retained after the callback or used for mutations.
func (s *DecisionStore) ReadHistory(ctx context.Context, fn func(HistoryRepositories) error) error {
	if s == nil || s.configuration == nil || fn == nil {
		return ErrDecisionAuthorizationUnavailable
	}
	c := s.configuration
	return mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		return fn(HistoryRepositories{Scenarios: ScenarioRepository{r}, Decisions: DecisionRepository{r}, RuleExecutions: RuleExecutionRepository{r}, TestRuns: TestRunRepository{r}, RuleSnoozes: RuleSnoozeRepository{r}, WorkflowExecutions: WorkflowExecutionRepository{r}, ScoringRequests: ScoringRequestRepository{providerExecutionRepository{r, false}}, OutboxEvents: OutboxEventRepository{r}})
	})
}
