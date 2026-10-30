package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/jackc/pgx/v5"
)

type publicationRepo struct {
	nilOutboxEventRepository
	events []integration.OutboxEvent
	err    error
}

func (r *publicationRepo) CreateMany(_ context.Context, events []integration.OutboxEvent) ([]integration.OutboxEvent, error) {
	r.events = append(r.events, events...)
	return events, r.err
}

type publicationEnqueuer struct {
	calls         int
	tenant, event string
	err           error
}

func (e *publicationEnqueuer) Enqueue(_ context.Context, tenant, event string, _ *time.Time) error {
	e.calls++
	e.tenant, e.event = tenant, event
	return e.err
}

func (e *publicationEnqueuer) EnqueueTx(ctx context.Context, _ pgx.Tx, tenant, event string, at *time.Time) error {
	return e.Enqueue(ctx, tenant, event, at)
}

func publicationService(enabled bool, repo ports.OutboxEventRepository, enqueuer *publicationEnqueuer) DecisionService {
	svc := newFailureTestDecisionService(txManagerStub{store: decisionMutationStoreStub{
		enqueue: func(ctx context.Context, job ports.JobRequest) error {
			if enqueuer == nil {
				return errors.New("required outbox producer missing")
			}
			return enqueuer.Enqueue(ctx, job.TenantID, job.ResourceID, job.ScheduledAt)
		},
		decisionRepo: decisionCreateRepoStub{}, ruleExecutionRepo: nilRuleExecutionRepository(nil),
		workflowExecutionRepo: workflowExecutionRepoStub{}, screeningExecutionRepo: nilScreeningExecutionRepository(nil),
		scoringRequestRepo: nilScoringRequestRepository(nil), outboxRepo: repo,
	}}, nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), nilScreeningConfigRepository(nil))
	svc.publishOutboxEvents = enabled
	return svc
}

func TestOutboxPublicationPolicyDoesNotSkipDecisions(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			repo, queue := &publicationRepo{}, &publicationEnqueuer{}
			svc := publicationService(enabled, repo, queue)
			result, err := svc.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", failureTestEvaluationRequest())
			if err != nil || result.Decision == nil {
				t.Fatalf("decision missing: %v", err)
			}
			want := 0
			if enabled {
				want = 1
			}
			if len(repo.events) != want || queue.calls != want {
				t.Fatalf("events=%d jobs=%d, want %d", len(repo.events), queue.calls, want)
			}
			if enabled && (repo.events[0].TenantID != "tenant-1" || queue.tenant != "tenant-1" || queue.event != repo.events[0].ID || repo.events[0].EventType != "decision.created") {
				t.Fatal("tenant/event contract mismatch")
			}
		})
	}
}

func TestEnabledOutboxPropagatesWriteAndQueueErrors(t *testing.T) {
	failure := errors.New("publication failure")
	for _, stage := range []string{"insert", "enqueue", "missing_repository", "missing_enqueuer"} {
		t.Run(stage, func(t *testing.T) {
			repo, queue := &publicationRepo{}, &publicationEnqueuer{}
			if stage == "insert" {
				repo.err = failure
			}
			if stage == "enqueue" {
				queue.err = failure
			}
			svc := publicationService(true, repo, queue)
			if stage == "missing_repository" {
				svc.txManager = txManagerStub{store: decisionMutationStoreStub{decisionRepo: decisionCreateRepoStub{}, ruleExecutionRepo: nilRuleExecutionRepository(nil), workflowExecutionRepo: workflowExecutionRepoStub{}, screeningExecutionRepo: nilScreeningExecutionRepository(nil), scoringRequestRepo: nilScoringRequestRepository(nil)}}
			}
			if stage == "missing_enqueuer" {
				svc = publicationService(true, repo, nil)
			}
			_, err := svc.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", failureTestEvaluationRequest())
			if err == nil {
				t.Fatal("publication failure reported success")
			}
			if (stage == "insert" || stage == "enqueue") && !errors.Is(err, failure) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestExecutionLifecycleSharesDecisionPublicationPolicy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repo, queue := &publicationRepo{}, &publicationEnqueuer{}
		decisionSvc := publicationService(enabled, repo, queue)
		svc := ExecutionService{decisionSvc: decisionSvc, idGen: decisionSvc.idGen, clock: decisionSvc.clock, txManager: decisionSvc.txManager}
		err := svc.writeExecutionLifecycleEvents(context.Background(), nil, "tenant-1", "async_execution", "execution-1", "async.completed", map[string]any{"status": "completed"})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if enabled {
			want = 1
		}
		if len(repo.events) != want || queue.calls != want {
			t.Fatalf("enabled=%t events=%d jobs=%d", enabled, len(repo.events), queue.calls)
		}
	}
}
