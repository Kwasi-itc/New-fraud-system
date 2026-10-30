package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Test-only facades exercise ACTUAL SQL repositories via their authorized read
// snapshots. Embedded nil ports intentionally panic on an unexpected method;
// these fixtures are not successful production substitutes for unwired ports.
type serviceScenarioFixture struct {
	ports.ScenarioRepository
	store *AuthoringStore
}

func (r serviceScenarioFixture) GetByID(ctx context.Context, t, s string) (out scenario.Scenario, err error) {
	err = r.store.ReadSnapshot(ctx, func(v AuthoringRepositories) error { out, err = v.Scenarios.GetByID(ctx, t, s); return err })
	return
}

type serviceIterationFixture struct {
	ports.ScenarioIterationRepository
	store *AuthoringStore
}

func (r serviceIterationFixture) GetByID(ctx context.Context, t, s, i string) (out scenario.Iteration, err error) {
	err = r.store.ReadSnapshot(ctx, func(v AuthoringRepositories) error { out, err = v.Iterations.GetByID(ctx, t, s, i); return err })
	return
}

type serviceRuleFixture struct {
	ports.RuleRepository
	store *AuthoringStore
}

func (r serviceRuleFixture) ListByIteration(ctx context.Context, t, s, i string) (out []scenario.Rule, err error) {
	err = r.store.ReadSnapshot(ctx, func(v AuthoringRepositories) error { out, err = v.Rules.ListByIteration(ctx, t, s, i); return err })
	return
}

type serviceWorkflowFixture struct {
	ports.WorkflowRepository
	store *ConfigurationStore
}

func (r serviceWorkflowFixture) ListActiveByScenario(ctx context.Context, t, s string) (out []workflow.Definition, err error) {
	err = r.store.ReadSnapshot(ctx, func(v ConfigurationRepositories) error {
		out, err = v.Workflows.ListActiveByScenario(ctx, t, s)
		return err
	})
	return
}

type serviceWorkflowRuleFixture struct {
	ports.WorkflowRuleRepository
	store *ConfigurationStore
}

func (r serviceWorkflowRuleFixture) ListStructured(ctx context.Context, t, s string) (out []workflow.StructuredRule, err error) {
	err = r.store.ReadSnapshot(ctx, func(v ConfigurationRepositories) error {
		out, err = v.WorkflowRules.ListStructured(ctx, t, s)
		return err
	})
	return
}

type serviceScreeningFixture struct {
	ports.ScreeningConfigRepository
	store *ConfigurationStore
}

func (r serviceScreeningFixture) ListActiveByScenario(ctx context.Context, t, s string) (out []screening.Config, err error) {
	err = r.store.ReadSnapshot(ctx, func(v ConfigurationRepositories) error {
		out, err = v.ScreeningConfigs.ListActiveByScenario(ctx, t, s)
		return err
	})
	return
}

type serviceScoringFixture struct {
	ports.ScoringConfigRepository
	store *ConfigurationStore
}

func (r serviceScoringFixture) ListActiveByScenario(ctx context.Context, t, s string) (out []scoring.Config, err error) {
	err = r.store.ReadSnapshot(ctx, func(v ConfigurationRepositories) error {
		out, err = v.ScoringConfigs.ListActiveByScenario(ctx, t, s)
		return err
	})
	return
}

type serviceSnoozeFixture struct {
	ports.RuleSnoozeRepository
	store *DecisionStore
}

func (r serviceSnoozeFixture) ListActive(ctx context.Context, t, s, k, id string, at time.Time) (out []snooze.RuleSnooze, err error) {
	err = r.store.ReadSnapshot(ctx, func(v DecisionRepositories) error {
		out, err = v.RuleSnoozes.ListActive(ctx, t, s, k, id, at)
		return err
	})
	return
}

type serviceClockFixture struct{ at time.Time }

func (c serviceClockFixture) Now() time.Time { return c.at }

type serviceIDsFixture struct{}

func (serviceIDsFixture) New() uuid.UUID { return uuid.New() }

type serviceModelFixture struct {
	ports.DataModelReader
	tenant uuid.UUID
}

func (r serviceModelFixture) GetTenantModel(_ context.Context, tenant string) (ports.TenantModel, error) {
	if tenant != r.tenant.String() {
		return ports.TenantModel{}, ErrTenantScope
	}
	return ports.TenantModel{RevisionID: "fixture-model-v1", RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{
		"wallets": {Name: "wallets", Fields: map[string]ports.TenantModelField{"amount": {Type: "float"}}},
	}}, nil
}

func TestMySQLActualDecisionServiceThresholdsEvidenceEffectsAndRequiredJobRollback(t *testing.T) {
	for _, role := range []string{"installer_fixture", "api"} {
		t.Run(role, func(t *testing.T) { actualDecisionServiceReleaseCorpus(t, role) })
	}
}

func actualDecisionServiceReleaseCorpus(t *testing.T, role string) {
	f, u, _ := combinedFixture(t, true)
	ctx := context.Background()
	a := authoringStore(t, f.platformFixture, f.tenant, 500, 2<<20)
	draft := authoringDraft(f.tenant, f.parent)
	review, block, decline := 10, 20, 30
	draft.ScoreReviewThreshold, draft.ScoreBlockAndReviewThreshold, draft.ScoreDeclineThreshold = &review, &block, &decline
	if err := a.Mutate(ctx, func(r AuthoringRepositories) error {
		version, err := r.Iterations.NextVersion(ctx, f.tenant.String(), f.parent.String())
		if err != nil {
			return err
		}
		draft.Version = version
		if _, err = r.Iterations.Create(ctx, draft); err != nil {
			return err
		}
		for i, score := range []int{9, 1, 10, 10} {
			rule := authoringRule(f.tenant, draft.ID, i+1)
			rule.ScoreModifier = score
			rule.Formula = json.RawMessage(fmt.Sprintf(`{"function":"gte","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":%d}]}`, i+1))
			if _, err = r.Rules.Create(ctx, rule); err != nil {
				return err
			}
		}
		_, err = r.Iterations.Commit(ctx, f.tenant.String(), f.parent.String(), draft.ID, time.Now().UTC().Truncate(time.Microsecond))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Publish(ctx, PublicationInput{f.parent, uuid.MustParse(draft.ID), uuid.New(), uuid.New()}, func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	c, err := NewConfigurationStore(f.db, f.database, f.tenant, 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	f.screen.ConfigJSON = []byte(`{"queries":[{"name":"Fixture Person","type":"person"}]}`)
	if err := c.Mutate(ctx, func(r ConfigurationRepositories) error {
		_, err := r.ScreeningConfigs.Update(ctx, f.screen)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	runtimeDB := f.db
	if role == "api" {
		runtimeDB = releaseRuntimeAccount(t, f.platformFixture, role)
		runtimeFixture := f.platformFixture
		runtimeFixture.db = runtimeDB
		a = authoringStore(t, runtimeFixture, f.tenant, 500, 2<<20)
		c, err = NewConfigurationStore(runtimeDB, f.database, f.tenant, 500, 2<<20, allowAuthoringFixture)
		if err != nil {
			t.Fatal(err)
		}
		effects, err := NewQueuedEffectStore(runtimeDB, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
		if err != nil {
			t.Fatal(err)
		}
		u, err = NewDecisionUnitOfWorkStore(effects, executionTestStore(t, runtimeFixture, f.tenant))
		if err != nil {
			t.Fatal(err)
		}
	}
	history, err := NewDecisionStore(runtimeDB, f.database, f.tenant, 500, 2<<20, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewDecisionService(nil, serviceIDsFixture{}, serviceClockFixture{time.Now().UTC().Truncate(time.Microsecond)}, serviceModelFixture{tenant: f.tenant},
		serviceScenarioFixture{store: a}, serviceIterationFixture{store: a}, serviceRuleFixture{store: a}, nil, nil, nil,
		serviceWorkflowFixture{store: c}, serviceWorkflowRuleFixture{store: c}, nil, nil, nil, serviceSnoozeFixture{store: history}, nil, nil, nil, nil, nil,
		serviceScreeningFixture{store: c}, nil, serviceScoringFixture{store: c}, nil, true, "enabled", []string{"count", "sum", "avg"}, 1, 1, 1, nil)
	svc, err = svc.WithDecisionTransactions(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		amount, score int
		outcome       decision.Outcome
	}{
		{0, 0, decision.OutcomeApprove}, {1, 9, decision.OutcomeApprove}, {2, 10, decision.OutcomeReview}, {3, 20, decision.OutcomeBlockAndReview}, {4, 30, decision.OutcomeDecline},
	} {
		t.Run(string(test.outcome)+fmt.Sprint(test.score), func(t *testing.T) {
			req := service.DecisionEvaluationRequest{ObjectID: fmt.Sprintf("WALLET-%d", test.amount), ObjectType: "wallets", Fields: map[string]any{"amount": test.amount}}
			result, err := svc.EvaluateScenario(ctx, f.tenant.String(), f.parent.String(), req)
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision == nil || result.Decision.Score != test.score || result.Decision.Outcome != test.outcome || !result.Triggered || len(result.RuleExecutions) != 4 {
				t.Fatal("service result", result)
			}
			id := result.Decision.ID
			if err := history.ReadSnapshot(ctx, func(r DecisionRepositories) error {
				stored, err := r.Decisions.GetByID(ctx, f.tenant.String(), id)
				if err != nil {
					return err
				}
				if stored.Score != test.score || stored.ScenarioIterationID != draft.ID || stored.ObjectID != req.ObjectID || stored.Outcome != test.outcome || stored.TenantID != f.tenant.String() {
					t.Fatal("persisted decision", stored)
				}
				var body map[string]any
				if err := json.Unmarshal(stored.RequestBody, &body); err != nil {
					return err
				}
				if body["model_revision"] != "fixture-model-v1" {
					t.Fatal("model revision not captured")
				}
				evidence, err := r.RuleExecutions.ListByDecision(ctx, f.tenant.String(), id)
				if err != nil {
					return err
				}
				if len(evidence) != 4 {
					t.Fatal("evidence missing", evidence)
				}
				hits := 0
				for _, e := range evidence {
					if e.Outcome == "hit" {
						hits++
					}
					if e.Evaluation == nil || e.DecisionID != id {
						t.Fatal("evidence binding", e)
					}
				}
				if hits != test.amount {
					t.Fatal("wrong matches", hits)
				}
				if _, err := r.Decisions.GetByID(ctx, uuid.NewString(), id); !errors.Is(err, ErrTenantScope) {
					t.Fatal("foreign tenant read", err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Only review is configured for the three external effects. Its four
	// publication events plus all other decision-created events must also have
	// exact required deliveries, despite the source's explicit enqueue calls.
	for table, want := range map[string]int{"decision_engine_decisions": 5, "decision_engine_rule_executions": 20, "decision_engine_workflow_executions": 1, "decision_engine_screening_executions": 1, "decision_engine_scoring_requests": 1, "decision_engine_outbox_events": 8, "decision_engine_effect_job_receipts": 11} {
		if got := executionCount(t, f.platformFixture, table); got != want {
			t.Fatalf("%s got=%d want=%d", table, got, want)
		}
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_service_job_failure BEFORE INSERT ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required delivery failed'"); err != nil {
		t.Fatal(err)
	}
	request := service.DecisionEvaluationRequest{ObjectID: "ROLLBACK-WALLET", ObjectType: "wallets", Fields: map[string]any{"amount": 2}}
	if _, err := svc.EvaluateScenario(ctx, f.tenant.String(), f.parent.String(), request); err == nil {
		t.Fatal("job error swallowed")
	}
	for table, want := range map[string]int{"decision_engine_decisions": 5, "decision_engine_rule_executions": 20, "decision_engine_workflow_executions": 1, "decision_engine_effect_job_receipts": 11} {
		if got := executionCount(t, f.platformFixture, table); got != want {
			t.Fatalf("partial service writes in %s: %d", table, got)
		}
	}
	request.Fields = map[string]any{"amount": "invalid"}
	if _, err := svc.EvaluateScenario(ctx, f.tenant.String(), f.parent.String(), request); err == nil {
		t.Fatal("invalid payload accepted")
	}
	if _, err := svc.EvaluateScenario(ctx, uuid.NewString(), f.parent.String(), request); !errors.Is(err, ErrTenantScope) {
		t.Fatal("cross tenant request", err)
	}
}
