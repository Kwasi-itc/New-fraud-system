package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type TransactionManager struct {
	db *pgxpool.Pool
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
}

func (m TransactionManager) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	store := mutationStore{tx: tx}
	if err := fn(store); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type mutationStore struct {
	tx pgx.Tx
}

func (s mutationStore) Scenarios() ports.ScenarioRepository {
	return NewScenarioRepository(s.tx)
}

func (s mutationStore) Iterations() ports.ScenarioIterationRepository {
	return NewScenarioIterationRepository(s.tx)
}

func (s mutationStore) Publications() ports.ScenarioPublicationRepository {
	return NewScenarioPublicationRepository(s.tx)
}

func (s mutationStore) Rules() ports.RuleRepository {
	return NewRuleRepository(s.tx)
}

func (s mutationStore) Decisions() ports.DecisionRepository {
	return NewDecisionRepository(s.tx)
}

func (s mutationStore) RuleExecutions() ports.RuleExecutionRepository {
	return NewRuleExecutionRepository(s.tx)
}

func (s mutationStore) TestRuns() ports.TestRunRepository {
	return NewTestRunRepository(s.tx)
}

func (s mutationStore) PhantomDecisions() ports.PhantomDecisionRepository {
	return NewPhantomDecisionRepository(s.tx)
}

func (s mutationStore) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository {
	return NewPhantomRuleExecutionRepository(s.tx)
}

func (s mutationStore) Workflows() ports.WorkflowRepository {
	return NewWorkflowRepository(s.tx)
}

func (s mutationStore) WorkflowRules() ports.WorkflowRuleRepository {
	return NewWorkflowRuleRepository(s.tx)
}

func (s mutationStore) WorkflowConditions() ports.WorkflowConditionRepository {
	return NewWorkflowConditionRepository(s.tx)
}

func (s mutationStore) WorkflowActions() ports.WorkflowActionRepository {
	return NewWorkflowActionRepository(s.tx)
}

func (s mutationStore) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return NewWorkflowExecutionRepository(s.tx)
}

func (s mutationStore) RuleSnoozes() ports.RuleSnoozeRepository {
	return NewRuleSnoozeRepository(s.tx)
}

func (s mutationStore) OutboxEvents() ports.OutboxEventRepository {
	return NewOutboxEventRepository(s.tx)
}

func (s mutationStore) ScheduledExecutions() ports.ScheduledExecutionRepository {
	return NewScheduledExecutionRepository(s.tx)
}

func (s mutationStore) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return NewAsyncDecisionExecutionRepository(s.tx)
}

func (s mutationStore) ScreeningConfigs() ports.ScreeningConfigRepository {
	return NewScreeningConfigRepository(s.tx)
}

func (s mutationStore) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return NewScreeningExecutionRepository(s.tx)
}

func (s mutationStore) ScoringConfigs() ports.ScoringConfigRepository {
	return NewScoringConfigRepository(s.tx)
}

func (s mutationStore) ScoringRequests() ports.ScoringRequestRepository {
	return NewScoringRequestRepository(s.tx)
}

func (s mutationStore) CustomLists() ports.CustomListRepository {
	return NewCustomListRepository(s.tx)
}

func (s mutationStore) RecordTags() ports.RecordTagRepository {
	return NewRecordTagRepository(s.tx)
}

func (s mutationStore) RiskSnapshots() ports.RiskSnapshotRepository {
	return NewRiskSnapshotRepository(s.tx)
}

func (s mutationStore) IPFlags() ports.IPFlagRepository {
	return NewIPFlagRepository(s.tx)
}
