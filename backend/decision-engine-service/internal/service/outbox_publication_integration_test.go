package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

// Every subtest creates and drops only a new, generated disposable database.
func TestIntegrationDecisionOutboxPublicationAtomicity(t *testing.T) {
	for _, mode := range []string{"disabled_without_river", "enabled", "queue_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			databaseURL := executionIntegrationDatabaseURL(t)
			runExecutionMetadataMigrations(t, databaseURL)
			pool := executionIntegrationPool(t, ctx, databaseURL)
			defer pool.Close()
			tenant, sid, iid, rid := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
			for _, stmt := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'outbox-test','transactions',now(),now())`, []any{sid, tenant}},
				{`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'committed',now())`, []any{iid, sid, tenant}},
				{`INSERT INTO core.rules(id,iteration_id,tenant_id,name,created_at,updated_at) VALUES($1,$2,$3,'hit',now(),now())`, []any{rid, iid, tenant}},
			} {
				if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
					t.Fatal(err)
				}
			}
			svc := newFailureTestDecisionService(storepostgres.NewTransactionManager(pool), nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), nilScreeningConfigRepository(nil))
			svc.idGen = &executionSequenceIDGenerator{next: executionIntegrationUUIDSequence(20)}
			svc.scenarioRepo = scenarioRepoStub{item: scenario.Scenario{ID: sid, TenantID: tenant, TriggerObjectType: "transactions", LiveIterationID: &iid}}
			svc.iterationRepo = scenarioIterationRepoStub{iteration: scenario.Iteration{ID: iid, ScenarioID: sid, TenantID: tenant, Status: scenario.IterationStatusCommitted, TriggerFormula: []byte(`{"constant":true}`), ScoreReviewThreshold: intPtrTest(10), ScoreBlockAndReviewThreshold: intPtrTest(20), ScoreDeclineThreshold: intPtrTest(30)}}
			svc.ruleRepo = ruleRepoStub{rules: []scenario.Rule{{ID: rid, IterationID: iid, TenantID: tenant, Name: "hit", ScoreModifier: 15, Formula: []byte(`{"constant":true}`)}}}
			svc.publishOutboxEvents = mode != "disabled_without_river"
			if svc.publishOutboxEvents {
				migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
					t.Fatal(err)
				}
				client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
				if err != nil {
					t.Fatal(err)
				}
				svc.txManager = storepostgres.NewTransactionManager(pool).WithJobEnqueuers(storepostgres.JobEnqueuers{
					Outbox: riverjobs.NewRiverOutboxEventEnqueuer(client, 3, riverjobs.OutboxEventQueueName),
				})
				if mode == "queue_failure" {
					// Fail the real INSERT, after decision/rules/event writes.
					if _, err := pool.Exec(ctx, `ALTER TABLE public.river_job ADD CONSTRAINT reject_test_outbox CHECK(kind <> 'outbox_event')`); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := svc.EvaluateScenario(ctx, tenant, sid, failureTestEvaluationRequest())
			if (err != nil) != (mode == "queue_failure") {
				t.Fatalf("mode=%s error=%v", mode, err)
			}
			want := 1
			if mode == "queue_failure" {
				want = 0
			}
			for _, table := range []string{"core.decisions", "core.rule_executions", "core.outbox_events"} {
				var count int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				expected := want
				if table == "core.outbox_events" && !svc.publishOutboxEvents {
					expected = 0
				}
				if count != expected {
					t.Fatalf("%s count=%d want=%d", table, count, expected)
				}
			}
			if mode == "enabled" {
				var raw []byte
				var jobs int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job`).Scan(&jobs); err != nil || jobs != 1 {
					t.Fatalf("jobs=%d err=%v", jobs, err)
				}
				if err := pool.QueryRow(ctx, `SELECT args FROM public.river_job WHERE kind='outbox_event' AND queue='outbox_events'`).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var args riverjobs.OutboxEventArgs
				if err := json.Unmarshal(raw, &args); err != nil {
					t.Fatal(err)
				}
				event, err := storepostgres.NewOutboxEventRepository(pool).GetByID(ctx, tenant, args.EventID)
				if err != nil || args.TenantID != tenant || event.AggregateID != result.Decision.ID {
					t.Fatalf("job/event mismatch: %v", err)
				}
				_, err = storepostgres.NewOutboxEventRepository(pool).GetByID(ctx, uuid.NewString(), args.EventID)
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("cross-tenant event visible: %v", err)
				}
			}
			if mode == "queue_failure" {
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rolled back jobs=%d err=%v", count, err)
				}
			}
		})
	}
}
