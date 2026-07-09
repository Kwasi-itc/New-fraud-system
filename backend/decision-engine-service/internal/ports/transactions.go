package ports

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type MutationStore interface {
	Scenarios() ScenarioRepository
	Iterations() ScenarioIterationRepository
	Publications() ScenarioPublicationRepository
	Rules() RuleRepository
	Decisions() DecisionRepository
	RuleExecutions() RuleExecutionRepository
	TestRuns() TestRunRepository
	PhantomDecisions() PhantomDecisionRepository
	PhantomRuleExecutions() PhantomRuleExecutionRepository
	Workflows() WorkflowRepository
	WorkflowRules() WorkflowRuleRepository
	WorkflowConditions() WorkflowConditionRepository
	WorkflowActions() WorkflowActionRepository
	WorkflowExecutions() WorkflowExecutionRepository
	RuleSnoozes() RuleSnoozeRepository
	OutboxEvents() OutboxEventRepository
	ScheduledExecutions() ScheduledExecutionRepository
	AsyncDecisionExecutions() AsyncDecisionExecutionRepository
	ScreeningConfigs() ScreeningConfigRepository
	ScreeningExecutions() ScreeningExecutionRepository
	ScoringConfigs() ScoringConfigRepository
	ScoringRequests() ScoringRequestRepository
	CustomLists() CustomListRepository
	RecordTags() RecordTagRepository
	RiskSnapshots() RiskSnapshotRepository
	IPFlags() IPFlagRepository
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(store MutationStore) error) error
}

type TransactionTimings struct {
	BeginMicros  int64
	BodyMicros   int64
	CommitMicros int64
}

type transactionTimingsContextKey struct{}

func WithTransactionTimings(ctx context.Context, timings *TransactionTimings) context.Context {
	return context.WithValue(ctx, transactionTimingsContextKey{}, timings)
}

func TransactionTimingsFromContext(ctx context.Context) (*TransactionTimings, bool) {
	timings, ok := ctx.Value(transactionTimingsContextKey{}).(*TransactionTimings)
	return timings, ok && timings != nil
}

type ScenarioRepository interface {
	Create(ctx context.Context, scenario scenario.Scenario) (scenario.Scenario, error)
	ListByTenant(ctx context.Context, tenantID string) ([]scenario.Scenario, error)
	ListLiveByTriggerObject(ctx context.Context, tenantID, objectType string) ([]scenario.Scenario, error)
	GetByID(ctx context.Context, tenantID, scenarioID string) (scenario.Scenario, error)
	Update(ctx context.Context, scenario scenario.Scenario) (scenario.Scenario, error)
	Delete(ctx context.Context, tenantID, scenarioID string) error
	SetLiveIterationID(ctx context.Context, tenantID, scenarioID string, iterationID *string) error
}

type ScenarioIterationRepository interface {
	Create(ctx context.Context, iteration scenario.Iteration) (scenario.Iteration, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Iteration, error)
	ListLiveScheduled(ctx context.Context, limit int) ([]scenario.Iteration, error)
	NextVersion(ctx context.Context, tenantID, scenarioID string) (int, error)
	GetByID(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.Iteration, error)
	Commit(ctx context.Context, tenantID, scenarioID, iterationID string, committedAt time.Time) (scenario.Iteration, error)
	Update(ctx context.Context, iteration scenario.Iteration) (scenario.Iteration, error)
}

type ScenarioPublicationRepository interface {
	Create(ctx context.Context, publication scenario.Publication) (scenario.Publication, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Publication, error)
}

type RuleRepository interface {
	Create(ctx context.Context, rule scenario.Rule) (scenario.Rule, error)
	ListByIteration(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Rule, error)
	ListRuleGroupsByScenario(ctx context.Context, tenantID, scenarioID string) ([]string, error)
	GetByID(ctx context.Context, tenantID, scenarioID, iterationID, ruleID string) (scenario.Rule, error)
	Update(ctx context.Context, rule scenario.Rule) (scenario.Rule, error)
	Delete(ctx context.Context, tenantID, scenarioID, iterationID, ruleID string) error
}

type DecisionRepository interface {
	Create(ctx context.Context, decision decision.Decision) (decision.Decision, error)
	GetByID(ctx context.Context, tenantID, decisionID string) (decision.Decision, error)
	ListByTenant(ctx context.Context, tenantID string) ([]decision.Decision, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]decision.Decision, error)
	ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]decision.Decision, error)
}

type RuleExecutionRepository interface {
	CreateMany(ctx context.Context, items []decision.RuleExecution) ([]decision.RuleExecution, error)
	ListByDecision(ctx context.Context, tenantID, decisionID string) ([]decision.RuleExecution, error)
}

type TestRunRepository interface {
	Create(ctx context.Context, testRun scenario.TestRun) (scenario.TestRun, error)
	GetByID(ctx context.Context, tenantID, testRunID string) (scenario.TestRun, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.TestRun, error)
	UpdateStatus(ctx context.Context, tenantID, testRunID string, status scenario.TestRunStatus, updatedAt time.Time) (scenario.TestRun, error)
}

type PhantomDecisionRepository interface {
	Create(ctx context.Context, item decision.PhantomDecision) (decision.PhantomDecision, error)
	ListByTestRun(ctx context.Context, tenantID, testRunID string) ([]decision.PhantomDecision, error)
}

type PhantomRuleExecutionRepository interface {
	CreateMany(ctx context.Context, items []decision.PhantomRuleExecution) ([]decision.PhantomRuleExecution, error)
	ListByPhantomDecision(ctx context.Context, tenantID, phantomDecisionID string) ([]decision.PhantomRuleExecution, error)
}

type WorkflowRepository interface {
	Create(ctx context.Context, item workflow.Definition) (workflow.Definition, error)
	GetByID(ctx context.Context, tenantID, scenarioID, workflowID string) (workflow.Definition, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error)
	ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error)
	Update(ctx context.Context, item workflow.Definition) (workflow.Definition, error)
	Reorder(ctx context.Context, tenantID, scenarioID string, orderedIDs []string, updatedAt time.Time) error
	Delete(ctx context.Context, tenantID, scenarioID, workflowID string) error
}

type WorkflowExecutionRepository interface {
	CreateMany(ctx context.Context, items []workflow.Execution) ([]workflow.Execution, error)
	ListByDecision(ctx context.Context, tenantID, decisionID string) ([]workflow.Execution, error)
	ListByStatus(ctx context.Context, status workflow.ExecutionStatus, limit int) ([]workflow.Execution, error)
	UpdateStatus(ctx context.Context, id string, status workflow.ExecutionStatus) error
}

type WorkflowRuleRepository interface {
	Create(ctx context.Context, item workflow.Rule) (workflow.Rule, error)
	GetByID(ctx context.Context, tenantID, scenarioID, ruleID string) (workflow.Rule, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Rule, error)
	Update(ctx context.Context, item workflow.Rule) (workflow.Rule, error)
	Reorder(ctx context.Context, tenantID, scenarioID string, orderedIDs []string, updatedAt time.Time) error
	Delete(ctx context.Context, tenantID, scenarioID, ruleID string) error
}

type WorkflowConditionRepository interface {
	Create(ctx context.Context, item workflow.Condition) (workflow.Condition, error)
	GetByID(ctx context.Context, tenantID, ruleID, conditionID string) (workflow.Condition, error)
	ListByRule(ctx context.Context, tenantID, ruleID string) ([]workflow.Condition, error)
	Update(ctx context.Context, item workflow.Condition) (workflow.Condition, error)
	Delete(ctx context.Context, tenantID, ruleID, conditionID string) error
}

type WorkflowActionRepository interface {
	Create(ctx context.Context, item workflow.Action) (workflow.Action, error)
	GetByID(ctx context.Context, tenantID, ruleID, actionID string) (workflow.Action, error)
	ListByRule(ctx context.Context, tenantID, ruleID string) ([]workflow.Action, error)
	Update(ctx context.Context, item workflow.Action) (workflow.Action, error)
	Delete(ctx context.Context, tenantID, ruleID, actionID string) error
}

type RuleSnoozeRepository interface {
	Create(ctx context.Context, item snooze.RuleSnooze) (snooze.RuleSnooze, error)
	ListActive(ctx context.Context, tenantID, scenarioID, objectType, objectID string, now time.Time) ([]snooze.RuleSnooze, error)
}

type OutboxEventRepository interface {
	CreateMany(ctx context.Context, items []integration.OutboxEvent) ([]integration.OutboxEvent, error)
	ListByTenant(ctx context.Context, tenantID string, limit int) ([]integration.OutboxEvent, error)
	ListByStatus(ctx context.Context, status integration.OutboxStatus, limit int) ([]integration.OutboxEvent, error)
	UpdateStatus(ctx context.Context, id string, status integration.OutboxStatus) error
}

type ScheduledExecutionRepository interface {
	Create(ctx context.Context, item execution.ScheduledExecution) (execution.ScheduledExecution, error)
	GetByID(ctx context.Context, tenantID, scenarioID, executionID string) (execution.ScheduledExecution, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]execution.ScheduledExecution, error)
	CountByStatus(ctx context.Context, tenantID, scenarioID string) (map[execution.Status]int, error)
	ListDue(ctx context.Context, now time.Time, limit int) ([]execution.ScheduledExecution, error)
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]execution.ScheduledExecution, error)
	UpdateStatus(ctx context.Context, id string, status execution.Status) error
	RecordAttemptFailure(ctx context.Context, id string, status execution.Status, nextAttemptAt *time.Time, lastError string, failedAt *time.Time) error
	ResetForRetry(ctx context.Context, id string, status execution.Status) error
}

type AsyncDecisionExecutionRepository interface {
	Create(ctx context.Context, item execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error)
	GetByID(ctx context.Context, tenantID, executionID string) (execution.AsyncDecisionExecution, error)
	ListByTenant(ctx context.Context, tenantID string) ([]execution.AsyncDecisionExecution, error)
	CountByStatus(ctx context.Context, tenantID string) (map[execution.Status]int, error)
	ListQueued(ctx context.Context, limit int) ([]execution.AsyncDecisionExecution, error)
	ClaimQueued(ctx context.Context, limit int) ([]execution.AsyncDecisionExecution, error)
	UpdateStatus(ctx context.Context, id string, status execution.Status) error
	RecordAttemptFailure(ctx context.Context, id string, status execution.Status, nextAttemptAt *time.Time, lastError string, failedAt *time.Time) error
	ResetForRetry(ctx context.Context, id string, status execution.Status) error
}

type ScreeningConfigRepository interface {
	Create(ctx context.Context, item screening.Config) (screening.Config, error)
	GetByID(ctx context.Context, tenantID, scenarioID, configID string) (screening.Config, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]screening.Config, error)
	ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]screening.Config, error)
	Update(ctx context.Context, item screening.Config) (screening.Config, error)
	Delete(ctx context.Context, tenantID, scenarioID, configID string) error
}

type ScreeningExecutionRepository interface {
	CreateMany(ctx context.Context, items []screening.Execution) ([]screening.Execution, error)
	GetByID(ctx context.Context, tenantID, executionID string) (screening.Execution, error)
	ListByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Execution, error)
	ListByStatus(ctx context.Context, status screening.ExecutionStatus, limit int) ([]screening.Execution, error)
	Update(ctx context.Context, item screening.Execution) (screening.Execution, error)
	UpdateStatus(ctx context.Context, id string, status screening.ExecutionStatus) error
}

type ScoringConfigRepository interface {
	Create(ctx context.Context, item scoring.Config) (scoring.Config, error)
	GetByID(ctx context.Context, tenantID, scenarioID, configID string) (scoring.Config, error)
	ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scoring.Config, error)
	ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]scoring.Config, error)
	Update(ctx context.Context, item scoring.Config) (scoring.Config, error)
	Delete(ctx context.Context, tenantID, scenarioID, configID string) error
}

type ScoringRequestRepository interface {
	CreateMany(ctx context.Context, items []scoring.Request) ([]scoring.Request, error)
	GetByID(ctx context.Context, tenantID, requestID string) (scoring.Request, error)
	ListByDecision(ctx context.Context, tenantID, decisionID string) ([]scoring.Request, error)
	ListByStatus(ctx context.Context, status scoring.RequestStatus, limit int) ([]scoring.Request, error)
	Update(ctx context.Context, item scoring.Request) (scoring.Request, error)
	UpdateStatus(ctx context.Context, id string, status scoring.RequestStatus) error
}

type CustomListRepository interface {
	CreateList(ctx context.Context, item platform.CustomList) (platform.CustomList, error)
	ListLists(ctx context.Context, tenantID string) ([]platform.CustomList, error)
	GetListByID(ctx context.Context, tenantID, listID string) (platform.CustomList, error)
	UpdateList(ctx context.Context, item platform.CustomList) (platform.CustomList, error)
	DeleteList(ctx context.Context, tenantID, listID string) error
	Create(ctx context.Context, item platform.CustomListEntry) (platform.CustomListEntry, error)
	ListEntriesByListID(ctx context.Context, tenantID, listID string) ([]platform.CustomListEntry, error)
	UpdateEntry(ctx context.Context, item platform.CustomListEntry) (platform.CustomListEntry, error)
	RenameEntriesByListID(ctx context.Context, tenantID, listID, listName string) error
	DeleteEntry(ctx context.Context, tenantID, listID, entryID string) error
	ListByName(ctx context.Context, tenantID, listName string) ([]platform.CustomListEntry, error)
	Contains(ctx context.Context, tenantID, listName, value string) (bool, error)
}

type RecordTagRepository interface {
	Create(ctx context.Context, item platform.RecordTag) (platform.RecordTag, error)
	ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]platform.RecordTag, error)
	HasTag(ctx context.Context, tenantID, objectType, objectID, tag string) (bool, error)
}

type RiskSnapshotRepository interface {
	Create(ctx context.Context, item platform.RiskSnapshot) (platform.RiskSnapshot, error)
	GetByObject(ctx context.Context, tenantID, objectType, objectID string) (*platform.RiskSnapshot, error)
}

type IPFlagRepository interface {
	Create(ctx context.Context, item platform.IPFlag) (platform.IPFlag, error)
	HasFlag(ctx context.Context, tenantID, ipAddress, flag string) (bool, error)
	ListByIP(ctx context.Context, tenantID, ipAddress string) ([]platform.IPFlag, error)
}
