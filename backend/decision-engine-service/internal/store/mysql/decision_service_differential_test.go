package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Actual services and SQL repositories on both engines. Model and authority
// providers are explicit fixtures. Configured effects and transactional queue
// writes are compared; external dispatch/provider conformance remains separate.
func TestActualDecisionServicePostgresMySQLThresholdEvidenceParity(t *testing.T) {
	actualDecisionServiceParity(t, false)
}

func TestActualDecisionServicePostgresMySQLConfiguredEffectParity(t *testing.T) {
	actualDecisionServiceParity(t, true)
}

// The source queue fixture persists on the actual transaction. It neither sends
// externally nor returns fake success for an absent queue producer.
type parityJobEnqueuer struct{ kind string }

func (e parityJobEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return fmt.Errorf("parity job must use the owner transaction")
}

func (e parityJobEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenant, id string, at *time.Time) error {
	_, err := tx.Exec(ctx, "INSERT INTO core.fixture_parity_jobs(kind,tenant_id,resource_id,scheduled_at) VALUES($1,$2,$3,$4)", e.kind, tenant, id, at)
	return err
}

type paritySequenceIDs struct {
	namespace uuid.UUID
	n         int
}

func (g *paritySequenceIDs) New() uuid.UUID {
	g.n++
	return uuid.NewSHA1(g.namespace, []byte(fmt.Sprint(g.n)))
}

func actualDecisionServiceParity(t *testing.T, withEffects bool, corpus ...scalarGoldenCase) {
	actualDecisionServiceParityMode(t, withEffects, false, corpus...)
}

func actualDecisionServiceParityMode(t *testing.T, withEffects, realLists bool, corpus ...scalarGoldenCase) {
	actualDecisionServiceFixture(t, withEffects, realLists, nil, corpus...)
}

// inspect runs after real service composition and before any evaluation. It lets
// race tests exercise the service boundary without duplicating migration setup.
func actualDecisionServiceFixture(t *testing.T, withEffects, realLists bool, inspect func(context.Context, effectFixture, *DecisionUnitOfWorkStore, service.DecisionService, uuid.UUID), corpus ...scalarGoldenCase) {
	ctx := context.Background()
	dsn := testsupport.PostgresDatabaseURL(t)
	dir, err := filepath.Abs("../../migrations/metadata")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if a, b := m.Close(); a != nil || b != nil {
		t.Fatal(a, b)
	}
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	f, unit, _ := combinedFixture(t, true)
	tenant, parent := f.tenant, uuid.New()
	authorize := AuthoringAuthorizer(allowAuthoringFixture)
	platformAuthorize := PlatformAuthorizer(allowPlatformFixture)
	var sourceLists, targetLists ports.EvaluationCustomLists
	var sourceTags, targetTags ports.EvaluationRecordTags
	var sourceRisk, targetRisk ports.EvaluationRiskSnapshots
	var sourceIP, targetIP ports.EvaluationIPFlags
	if realLists {
		authorize, platformAuthorize, sourceLists, targetLists, unit = prepareListIdentityParity(t, ctx, pg, f, unit)
		corpus = append(realListGoldenCorpus(), realPlatformGoldenCorpus()...)
		platformReads := preparePlatformParity(t, ctx, pg, f, platformAuthorize)
		sourceTags, sourceRisk, sourceIP = postgres.NewRecordTagRepository(pg), postgres.NewRiskSnapshotRepository(pg), postgres.NewIPFlagRepository(pg)
		targetTags, targetRisk, targetIP = platformReads, platformReads, platformReads
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 123456000, time.UTC)
	owner, err := NewAuthoringStore(f.db, f.database, tenant, 500, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	draft := authoringDraft(tenant, parent)
	draft.CreatedAt = at
	draft.TriggerFormula = []byte(`{"function":"gte","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":0}]}`)
	review, block, decline := 10, 20, 30
	draft.ScoreReviewThreshold, draft.ScoreBlockAndReviewThreshold, draft.ScoreDeclineThreshold = &review, &block, &decline
	rules := make([]scenario.Rule, 4)
	for i, score := range []int{9, 1, 10, 10} {
		rules[i] = authoringRule(tenant, draft.ID, i+1)
		rules[i].CreatedAt, rules[i].UpdatedAt = at, at
		rules[i].ScoreModifier = score
		rules[i].Formula = json.RawMessage(fmt.Sprintf(`{"function":"gte","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":%d}]}`, i+1))
	}
	snoozeGroup := uuid.NewString()
	rules[0].SnoozeGroupID = &snoozeGroup
	if len(corpus) > 0 {
		draft.TriggerFormula = json.RawMessage(`{"constant":true}`)
		rules = make([]scenario.Rule, len(corpus))
		for i, tc := range corpus {
			rules[i] = authoringRule(tenant, draft.ID, i+1)
			rules[i].Name = tc.name
			rules[i].CreatedAt, rules[i].UpdatedAt = at, at
			rules[i].ScoreModifier = 1
			// Only the selected golden case executes; every other rule must
			// short circuit and persist miss evidence.
			rules[i].Formula, err = json.Marshal(scalarCall("and", scalarCall("eq", scalarCall("payload", scalarConstant("amount")), scalarConstant(i)), tc.formula))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	seed := func(s authoringPorts) error {
		if _, err := s.scenarios.Create(ctx, scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Service differential", Description: "fixture", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		version, err := s.iterations.NextVersion(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		d := draft
		d.Version = version
		if _, err := s.iterations.Create(ctx, d); err != nil {
			return err
		}
		for _, rule := range rules {
			if _, err := s.rules.Create(ctx, rule); err != nil {
				return err
			}
		}
		_, err = s.iterations.Commit(ctx, tenant.String(), parent.String(), d.ID, at)
		return err
	}
	pgTX := postgres.NewTransactionManager(pg)
	if withEffects {
		if _, err := pg.Exec(ctx, "CREATE TABLE core.fixture_parity_jobs(kind TEXT NOT NULL,tenant_id UUID NOT NULL,resource_id UUID NOT NULL,scheduled_at TIMESTAMPTZ,PRIMARY KEY(kind,tenant_id,resource_id))"); err != nil {
			t.Fatal(err)
		}
		pgTX = pgTX.WithJobEnqueuers(postgres.JobEnqueuers{Workflow: parityJobEnqueuer{string(ports.JobWorkflow)}, Screening: parityJobEnqueuer{string(ports.JobScreening)}, Scoring: parityJobEnqueuer{string(ports.JobScoring)}, Outbox: parityJobEnqueuer{string(ports.JobOutbox)}})
	}
	if err := pgTX.Run(ctx, func(s ports.MutationStore) error {
		if err := seed(authoringPorts{s.Scenarios(), s.Iterations(), s.Rules()}); err != nil {
			return err
		}
		return s.Scenarios().SetLiveIterationID(ctx, tenant.String(), parent.String(), &draft.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Mutate(ctx, func(s AuthoringRepositories) error { return seed(authoringPorts{s.Scenarios, s.Iterations, s.Rules}) }); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Publish(ctx, PublicationInput{parent, uuid.MustParse(draft.ID), uuid.New(), uuid.New()}, func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	config, err := NewConfigurationStore(f.db, f.database, tenant, 500, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if withEffects {
		wf, screen, score := f.wf, f.screen, f.score
		wf.ID, wf.ScenarioID = uuid.NewString(), parent.String()
		screen.ID, screen.ScenarioID = uuid.NewString(), parent.String()
		score.ID, score.ScenarioID = uuid.NewString(), parent.String()
		screen.ConfigJSON = []byte(`{"queries":[{"name":"Fixture Person","type":"person"}]}`)
		if err := pgTX.Run(ctx, func(r ports.MutationStore) error {
			if _, e := r.Workflows().Create(ctx, wf); e != nil {
				return e
			}
			if _, e := r.ScreeningConfigs().Create(ctx, screen); e != nil {
				return e
			}
			_, e := r.ScoringConfigs().Create(ctx, score)
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if err := config.Mutate(ctx, func(r ConfigurationRepositories) error {
			if _, e := r.Workflows.Create(ctx, wf); e != nil {
				return e
			}
			if _, e := r.ScreeningConfigs.Create(ctx, screen); e != nil {
				return e
			}
			_, e := r.ScoringConfigs.Create(ctx, score)
			return e
		}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := NewDecisionStore(f.db, f.database, tenant, 500, 2<<20, platformAuthorize)
	if err != nil {
		t.Fatal(err)
	}
	namespace := uuid.New()
	source := service.NewDecisionService(pgTX, &paritySequenceIDs{namespace: namespace}, serviceClockFixture{at}, serviceModelFixture{tenant: tenant},
		postgres.NewScenarioRepository(pg), postgres.NewScenarioIterationRepository(pg), postgres.NewRuleRepository(pg), nil, postgres.NewDecisionRepository(pg), postgres.NewRuleExecutionRepository(pg),
		postgres.NewWorkflowRepository(pg), postgres.NewWorkflowRuleRepository(pg), nil, nil, nil, postgres.NewRuleSnoozeRepository(pg), nil, sourceLists, sourceTags, sourceRisk, sourceIP,
		postgres.NewScreeningConfigRepository(pg), nil, postgres.NewScoringConfigRepository(pg), nil, withEffects, "enabled", []string{"count", "sum", "avg"}, 1, 1, 1, nil)
	target := service.NewDecisionService(nil, &paritySequenceIDs{namespace: namespace}, serviceClockFixture{at}, serviceModelFixture{tenant: tenant},
		serviceScenarioFixture{store: owner}, serviceIterationFixture{store: owner}, serviceRuleFixture{store: owner}, nil, nil, nil,
		serviceWorkflowFixture{store: config}, serviceWorkflowRuleFixture{store: config}, nil, nil, nil, serviceSnoozeFixture{store: history}, nil, targetLists, targetTags, targetRisk, targetIP,
		serviceScreeningFixture{store: config}, nil, serviceScoringFixture{store: config}, nil, withEffects, "enabled", []string{"count", "sum", "avg"}, 1, 1, 1, nil)
	target, err = target.WithDecisionTransactions(unit)
	if err != nil {
		t.Fatal(err)
	}
	if inspect != nil {
		inspect(ctx, f, unit, target, parent)
		return
	}
	if len(corpus) > 0 {
		runScalarGoldenParity(t, ctx, source, target, pg, history, tenant, parent, rules, corpus)
		return
	}
	for _, amount := range []int{-1, 0, 1, 2, 3, 4} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			request := service.DecisionEvaluationRequest{ObjectID: fmt.Sprintf("wallet-%d", amount), ObjectType: "wallets", Fields: map[string]any{"amount": amount}}
			left, err := source.EvaluateScenario(ctx, tenant.String(), parent.String(), request)
			if err != nil {
				t.Fatal(err)
			}
			right, err := target.EvaluateScenario(ctx, tenant.String(), parent.String(), request)
			if err != nil {
				t.Fatal(err)
			}
			if amount < 0 {
				if left.Triggered || right.Triggered || left.Decision != nil || right.Decision != nil {
					t.Fatal("non-triggering request created a decision")
				}
			} else {
				scores := []int{0, 9, 10, 20, 30}
				outcomes := []decision.Outcome{decision.OutcomeApprove, decision.OutcomeApprove, decision.OutcomeReview, decision.OutcomeBlockAndReview, decision.OutcomeDecline}
				for _, result := range []service.DecisionEvaluationResult{left, right} {
					if !result.Triggered || result.Decision == nil || result.Decision.Score != scores[amount] || result.Decision.Outcome != outcomes[amount] || len(result.RuleExecutions) != 4 {
						t.Fatal("incorrect threshold/evidence result", result)
					}
				}
			}
			if !reflect.DeepEqual(normalizedServiceResult(t, left), normalizedServiceResult(t, right)) {
				t.Fatalf("service mismatch: source=%+v target=%+v", left, right)
			}
			if left.Decision == nil {
				return
			}
			if withEffects {
				want := 0
				if amount == 2 {
					want = 1
				}
				compareServiceEffects(t, ctx, pg, f.store, tenant.String(), left.Decision.ID, right.Decision.ID, want)
			}
			left.Decision, right.Decision = new(decision.Decision), new(decision.Decision)
			*left.Decision, err = postgres.NewDecisionRepository(pg).GetByID(ctx, tenant.String(), left.RuleExecutions[0].DecisionID)
			if err != nil {
				t.Fatal(err)
			}
			left.RuleExecutions, err = postgres.NewRuleExecutionRepository(pg).ListByDecision(ctx, tenant.String(), left.Decision.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := history.ReadSnapshot(ctx, func(r DecisionRepositories) error {
				var err error
				*right.Decision, err = r.Decisions.GetByID(ctx, tenant.String(), right.RuleExecutions[0].DecisionID)
				if err != nil {
					return err
				}
				right.RuleExecutions, err = r.RuleExecutions.ListByDecision(ctx, tenant.String(), right.Decision.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalizedServiceResult(t, left), normalizedServiceResult(t, right)) {
				t.Fatal("persisted decision/evidence mismatch")
			}
		})
	}
	if withEffects {
		for _, failedTable := range []string{"rule_executions", "workflow_executions", "screening_executions", "scoring_requests", "outbox_events"} {
			t.Run(failedTable+" failure rolls back both engines", func(t *testing.T) {
				if _, e := pg.Exec(ctx, "ALTER TABLE core."+failedTable+" ADD CONSTRAINT fixture_reject_insert CHECK(id IS NULL) NOT VALID"); e != nil {
					t.Fatal(e)
				}
				defer func() {
					if _, e := pg.Exec(ctx, "ALTER TABLE core."+failedTable+" DROP CONSTRAINT fixture_reject_insert"); e != nil {
						t.Error(e)
					}
				}()
				schema, _ := mysqlstore.Identifier(f.database)
				if _, e := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_insert_failure BEFORE INSERT ON "+schema+".decision_engine_"+failedTable+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected insert failure'"); e != nil {
					t.Fatal(e)
				}
				defer func() {
					if _, e := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_insert_failure"); e != nil {
						t.Error(e)
					}
				}()
				for _, svc := range []service.DecisionService{source, target} {
					result, e := svc.EvaluateScenario(ctx, tenant.String(), parent.String(), service.DecisionEvaluationRequest{ObjectID: "must rollback", ObjectType: "wallets", Fields: map[string]any{"amount": 2}})
					if e == nil || result.Decision != nil {
						t.Fatal("failed persistence returned provisional decision", result, e)
					}
				}
				for _, tc := range []struct {
					pg, my string
					want   int
				}{
					{"decisions", "decision_engine_decisions", 5}, {"rule_executions", "decision_engine_rule_executions", 20},
					{"workflow_executions", "decision_engine_workflow_executions", 1}, {"screening_executions", "decision_engine_screening_executions", 1},
					{"scoring_requests", "decision_engine_scoring_requests", 1}, {"outbox_events", "decision_engine_outbox_events", 8},
					{"fixture_parity_jobs", "decision_engine_effect_job_receipts", 11},
				} {
					var n int
					if e := pg.QueryRow(ctx, "SELECT COUNT(*) FROM core."+tc.pg).Scan(&n); e != nil || n != tc.want {
						t.Fatal(tc.pg, n, e)
					}
					if n := executionCount(t, f.platformFixture, tc.my); n != tc.want {
						t.Fatal(tc.my, n)
					}
				}
			})
		}
		t.Run("required job failure rolls back both engines", func(t *testing.T) {
			var jobs int
			if e := pg.QueryRow(ctx, "SELECT COUNT(*) FROM core.fixture_parity_jobs").Scan(&jobs); e != nil || jobs != 11 {
				t.Fatal(jobs, e)
			}
			if n := executionCount(t, f.platformFixture, "decision_engine_effect_job_receipts"); n != 11 {
				t.Fatal("target required jobs", n)
			}
			if _, e := pg.Exec(ctx, "ALTER TABLE core.fixture_parity_jobs ADD CONSTRAINT fixture_reject_new_jobs CHECK(resource_id IS NULL) NOT VALID"); e != nil {
				t.Fatal(e)
			}
			schema, _ := mysqlstore.Identifier(f.database)
			if _, e := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_parity_job_failure BEFORE INSERT ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required parity job failure'"); e != nil {
				t.Fatal(e)
			}
			for _, svc := range []service.DecisionService{source, target} {
				if result, e := svc.EvaluateScenario(ctx, tenant.String(), parent.String(), service.DecisionEvaluationRequest{ObjectID: "must rollback", ObjectType: "wallets", Fields: map[string]any{"amount": 2}}); e == nil || result.Decision != nil {
					t.Fatal("failed queue returned success/provisional decision", result, e)
				}
			}
			for _, tc := range []struct {
				pg, my string
				want   int
			}{{"decisions", "decision_engine_decisions", 5}, {"rule_executions", "decision_engine_rule_executions", 20}, {"workflow_executions", "decision_engine_workflow_executions", 1}, {"screening_executions", "decision_engine_screening_executions", 1}, {"scoring_requests", "decision_engine_scoring_requests", 1}, {"outbox_events", "decision_engine_outbox_events", 8}, {"fixture_parity_jobs", "decision_engine_effect_job_receipts", 11}} {
				var n int
				if e := pg.QueryRow(ctx, "SELECT COUNT(*) FROM core."+tc.pg).Scan(&n); e != nil || n != tc.want {
					t.Fatal(tc.pg, n, e)
				}
				if n := executionCount(t, f.platformFixture, tc.my); n != tc.want {
					t.Fatal(tc.my, n)
				}
			}
		})
	}
	for _, svc := range []service.DecisionService{source, target} {
		if _, err := svc.EvaluateScenario(ctx, tenant.String(), parent.String(), service.DecisionEvaluationRequest{ObjectID: "invalid", ObjectType: "wallets", Fields: map[string]any{"amount": "bad"}}); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	var sourceCount int
	if err := pg.QueryRow(ctx, "SELECT COUNT(*) FROM core.decisions WHERE tenant_id=$1 AND scenario_id=$2", tenant, parent).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if err := history.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		n, err := r.Decisions.CountByScenario(ctx, tenant.String(), parent.String())
		if err == nil && (n != 5 || n != sourceCount) {
			t.Fatalf("failed/non-triggered request persisted: PG=%d MySQL=%d", sourceCount, n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !withEffects {
		t.Run("snoozed rule scoring source normalization case and exclusive expiry", func(t *testing.T) {
			for _, object := range []string{"snoozed", "expired"} {
				expires := at.Add(time.Hour)
				if object == "expired" {
					expires = at
				}
				sn := snooze.RuleSnooze{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), SnoozeGroupID: snoozeGroup, ObjectType: "wallets", ObjectID: object, CreatedAt: at.Add(-time.Hour), ExpiresAt: expires}
				if err := pgTX.Run(ctx, func(s ports.MutationStore) error { _, e := s.RuleSnoozes().Create(ctx, sn); return e }); err != nil {
					t.Fatal(err)
				}
				if err := history.Mutate(ctx, func(r DecisionRepositories) error { _, e := r.RuleSnoozes.Create(ctx, sn); return e }); err != nil {
					t.Fatal(err)
				}
			}
			for _, object := range []string{"snoozed", "snoozed ", "Snoozed", "expired"} {
				request := service.DecisionEvaluationRequest{ObjectID: object, ObjectType: "wallets", Fields: map[string]any{"amount": 4}}
				left, err := source.EvaluateScenario(ctx, tenant.String(), parent.String(), request)
				if err != nil {
					t.Fatal(err)
				}
				right, err := target.EvaluateScenario(ctx, tenant.String(), parent.String(), request)
				if err != nil {
					t.Fatal(err)
				}
				want := 30
				if object == "snoozed " || object == "snoozed" {
					want = 21
				}
				if left.Decision == nil || right.Decision == nil || left.Decision.Score != want || right.Decision.Score != want || !reflect.DeepEqual(normalizedServiceResult(t, left), normalizedServiceResult(t, right)) {
					t.Fatalf("snooze evaluation mismatch for %q: %+v %+v", object, left, right)
				}
				for _, result := range []service.DecisionEvaluationResult{left, right} {
					found := false
					for _, e := range result.RuleExecutions {
						if e.RuleID == rules[0].ID {
							found = true
							wantOutcome := "hit"
							if object == "snoozed " || object == "snoozed" {
								wantOutcome = "snoozed"
							}
							if e.Outcome != wantOutcome {
								t.Fatal("wrong snooze evidence", e)
							}
						}
					}
					if !found {
						t.Fatal("missing rule evidence")
					}
				}
			}
		})
	}
}

func compareServiceEffects(t *testing.T, ctx context.Context, pg *pgxpool.Pool, target *EffectStore, tenant, leftID, rightID string, want int) {
	t.Helper()
	type snapshot struct {
		Workflows []workflow.Execution
		Screening []screening.Execution
		Scoring   []scoring.Request
		Outbox    []integration.OutboxEvent
	}
	read := func(p differentialEffectPorts, id string) (snapshot, error) {
		out := snapshot{Workflows: []workflow.Execution{}, Screening: []screening.Execution{}, Scoring: []scoring.Request{}, Outbox: []integration.OutboxEvent{}}
		wf, e := p.workflow.ListByDecision(ctx, tenant, id)
		if e != nil {
			return out, e
		}
		out.Workflows = append(out.Workflows, wf...)
		sc, e := p.screen.ListByDecision(ctx, tenant, id)
		if e != nil {
			return out, e
		}
		out.Screening = append(out.Screening, sc...)
		score, e := p.score.ListByDecision(ctx, tenant, id)
		if e != nil {
			return out, e
		}
		out.Scoring = append(out.Scoring, score...)
		events, e := p.outbox.ListByTenant(ctx, tenant, 100)
		if e != nil {
			return out, e
		}
		out.Outbox = append(out.Outbox, events...)
		if len(wf) != want || len(sc) != want || len(score) != want {
			t.Fatalf("configured effect counts: workflow=%d screening=%d scoring=%d want=%d", len(wf), len(sc), len(score), want)
		}
		sort.Slice(out.Outbox, func(i, j int) bool { return out.Outbox[i].ID < out.Outbox[j].ID })
		return out, nil
	}
	left, e := read(differentialEffectPorts{workflow: postgres.NewWorkflowExecutionRepository(pg), screen: postgres.NewScreeningExecutionRepository(pg), score: postgres.NewScoringRequestRepository(pg), outbox: postgres.NewOutboxEventRepository(pg)}, leftID)
	if e != nil {
		t.Fatal(e)
	}
	var right snapshot
	if e := target.ReadSnapshot(ctx, func(r EffectRepositories) error {
		var e error
		right, e = read(differentialEffectPorts{workflow: r.WorkflowExecutions, screen: r.ScreeningExecutions, score: r.ScoringRequests, outbox: r.OutboxEvents}, rightID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	canonical := func(v any) any {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var out any
		if e := d.Decode(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	if !reflect.DeepEqual(canonical(left), canonical(right)) {
		t.Fatalf("actual service effect mismatch\nPG=%+v\nMySQL=%+v", left, right)
	}
}

func normalizedServiceResult(t *testing.T, result service.DecisionEvaluationResult) any {
	t.Helper()
	if result.Decision != nil {
		copy := *result.Decision
		copy.ID = "generated"
		result.Decision = &copy
	}
	result.RuleExecutions = append([]decision.RuleExecution(nil), result.RuleExecutions...)
	for i := range result.RuleExecutions {
		result.RuleExecutions[i].ID, result.RuleExecutions[i].DecisionID = "generated", "generated"
	}
	sort.Slice(result.RuleExecutions, func(i, j int) bool { return result.RuleExecutions[i].RuleID < result.RuleExecutions[j].RuleID })
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		t.Fatal(err)
	}
	return normalized
}
