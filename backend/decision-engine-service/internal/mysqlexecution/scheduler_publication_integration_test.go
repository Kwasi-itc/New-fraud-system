package mysqlexecution

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/recurrence"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func TestMySQLSchedulerBoundedPagesAtomicityAndConcurrentTicks(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	s, err := NewScheduler(f.executions, SchedulerPolicy{PageSize: 1, OccurrencesPerSchedule: 2, Interval: 10 * time.Millisecond, TickTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		v := mysql.ExecutionScheduleInput{ID: uuid.New(), ScenarioID: f.parent, ScenarioIterationID: f.old, StartAt: clock{}.Now().Add(-3 * time.Hour), Interval: time.Hour, Policy: workers.CatchUp, MaxAttempts: 3, RequestBody: []byte(`{"items":[]}`), Active: true}
		if i == 1 {
			// One active schedule per scenario is an owner invariant. Give the
			// second schedule its own real committed/published scenario.
			original := f.parent
			f.parent = uuid.New()
			if err := f.authoring.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
				_, err := r.Scenarios.Create(ctx, scenario.Scenario{ID: f.parent.String(), TenantID: f.tenant.String(), Name: "Calendar", Description: "fixture", TriggerObjectType: "wallets", CreatedAt: clock{}.Now(), UpdatedAt: clock{}.Now()})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			v.ScenarioID, v.ScenarioIterationID = f.parent, f.publish(t, 15)
			f.parent = original
			v.StartAt = time.Time{}
			v.Interval = 0
			v.Calendar = &recurrence.Spec{Version: 1, Frequency: "monthly", Timezone: "UTC", DayOfMonth: 31, TimeOfDay: "09:30", GapPolicy: recurrence.SkipGap, OverlapPolicy: recurrence.FirstOverlap}
			v.StartLocal = "2020-01-31T09:30"
		}
		if err := f.executions.Run(ctx, mysql.ExecutionScheduleAdmin, func(r mysql.ExecutionRepositories) error { _, err := r.Schedules.Create(ctx, v); return err }); err != nil {
			t.Fatal(err)
		}
	}
	report := func(context.Context, error) error { t.Error("unexpected scheduling skip"); return nil }
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".fixture_tick_fail BEFORE INSERT ON " + f.schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='jobs unavailable'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(ctx, nil, report); err == nil || f.count(t, "decision_engine_schedule_occurrences") != 0 {
		t.Fatal("failed tick advanced watermark", err)
	}
	if _, err := f.db.Exec("DROP TRIGGER " + f.schema + ".fixture_tick_fail"); err != nil {
		t.Fatal(err)
	}
	cursor, err := s.Tick(ctx, nil, report)
	if err != nil || cursor == nil || f.count(t, "decision_engine_schedule_occurrences") != 2 {
		t.Fatal(cursor, err)
	}
	_, err = s.Tick(ctx, cursor, report)
	if err != nil || f.count(t, "decision_engine_schedule_occurrences") != 4 {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Tick(ctx, nil, report); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.count(t, "decision_engine_schedule_occurrences") != 8 || f.count(t, "decision_engine_scheduled_executions") != 8 {
		t.Fatal("concurrent tick lost/duplicated outputs")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Run(cancelled, report); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMySQLPublicationFullValidationModelClosureAndRace(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	p, err := NewPublication(f.authoring, f.preparer.reader, f.preparer.metadataLimits)
	if err != nil {
		t.Fatal(err)
	}
	input := mysql.PublicationInput{ScenarioID: f.parent, IterationID: f.old, EventID: uuid.New(), ReplacedEventID: uuid.New()}
	before := f.count(t, "decision_engine_scenario_publications")
	if _, err := p.Publish(ctx, input); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "decision_engine_scenario_publications") != before+1 {
		t.Fatal("publication event missing")
	}
	if _, err := f.db.Exec("UPDATE " + f.schema + ".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	input.EventID = uuid.New()
	input.ReplacedEventID = uuid.New()
	if _, err := p.Publish(ctx, input); err == nil {
		t.Fatal("closed trigger table published without aggregation")
	}
	if f.count(t, "decision_engine_scenario_publications") != before+1 {
		t.Fatal("failed publication wrote event")
	}
	if _, err := f.db.Exec("UPDATE " + f.schema + ".data_model_physical_tables SET write_state='open'"); err != nil {
		t.Fatal(err)
	}
	f.provider.onRead = func() { f.publish(t, 99) }
	if _, err := p.Publish(ctx, input); !errors.Is(err, mysql.ErrExecutionBindingChanged) {
		t.Fatal("changed parent/live binding accepted", err)
	}
	f.provider.onRead = nil
	bad := uuid.New()
	if _, err := f.authoring.CloneIteration(ctx, f.parent, f.old, bad); err != nil {
		t.Fatal(err)
	}
	if err := f.authoring.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		rules, err := r.Rules.ListByIteration(ctx, f.tenant.String(), f.parent.String(), bad.String())
		if err != nil {
			return err
		}
		rules[0].Formula = []byte(`{"function":"eq","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":"not a number"}]}`)
		if _, err := r.Rules.Update(ctx, rules[0]); err != nil {
			return err
		}
		_, err = r.Iterations.Commit(ctx, f.tenant.String(), f.parent.String(), bad.String(), clock{}.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	input.IterationID = bad
	if _, err := p.Publish(ctx, input); err == nil {
		t.Fatal("invalid complete formula published")
	}
}
