package ports

import "context"

// DecisionMutationStore is the persistence capability needed by live evaluation,
// not the legacy all-service MutationStore. In particular, it cannot expose
// execution methods which omit a worker's lease/attempt/generation fence.
// Required jobs are persisted on the exact caller transaction; failure must
// roll back its decision/evidence/effects. External I/O belongs outside it.
type DecisionMutationStore interface {
	Decisions() DecisionRepository
	RuleExecutions() RuleExecutionRepository
	WorkflowExecutions() WorkflowExecutionRepository
	ScreeningExecutions() ScreeningExecutionRepository
	ScoringRequests() ScoringRequestRepository
	OutboxEvents() OutboxEventRepository
	Enqueue(context.Context, JobRequest) error
}

type DecisionTransactionManager interface {
	RunDecisionMutation(context.Context, func(DecisionMutationStore) error) error
}

// DecisionJobBatchEnqueuer is an optional bounded producer capability. All jobs
// still belong to the caller's decision transaction and require owner-state and
// tenant checks. An error must abort that transaction; callers must not retry
// individual jobs after a failed batch. Legacy adapters can retain Enqueue.
type DecisionJobBatchEnqueuer interface {
	EnqueueBatch(context.Context, []JobRequest) error
}
