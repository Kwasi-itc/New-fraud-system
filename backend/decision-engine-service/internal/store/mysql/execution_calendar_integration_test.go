package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/recurrence"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func TestMySQLExecutionCalendarAtomicityCivilTimesAndTenantIsolation(t *testing.T) {
	f, store, tenant, parent, iteration := executionFixture(t)
	schema, _ := mysqlstore.Identifier(f.database)
	ctx := context.Background()
	var expectedRecords int
	for i, test := range []struct {
		frequency, zone, clock, weekday, anchor, next string
		day                                           int
		times                                         []string
		skipped                                       int64
	}{
		{"monthly", "UTC", "09:30", "", "2020-01-31T09:30", "2020-04-30T09:30", 31, []string{"2020-01-31T09:30:00Z", "2020-02-29T09:30:00Z", "2020-03-31T09:30:00Z"}, 0},
		{"weekly", "America/Los_Angeles", "23:30", "sunday", "2020-01-05T23:30", "2020-01-26T23:30", 0, []string{"2020-01-06T07:30:00Z", "2020-01-13T07:30:00Z", "2020-01-20T07:30:00Z"}, 0},
		{"daily", "America/New_York", "02:30", "", "2020-03-07T02:30", "2020-03-11T02:30", 0, []string{"2020-03-07T07:30:00Z", "2020-03-09T06:30:00Z", "2020-03-10T06:30:00Z"}, 1},
		{"hourly", "America/New_York", "", "", "2020-11-01T00:30", "2020-11-01T03:30", 0, []string{"2020-11-01T04:30:00Z", "2020-11-01T05:30:00Z", "2020-11-01T07:30:00Z"}, 0},
	} {
		t.Run(test.frequency, func(t *testing.T) {
			spec := recurrence.Spec{Version: 1, Frequency: test.frequency, Timezone: test.zone, TimeOfDay: test.clock, DayOfWeek: test.weekday, DayOfMonth: test.day, GapPolicy: recurrence.SkipGap, OverlapPolicy: recurrence.FirstOverlap}
			if test.frequency == "hourly" {
				spec.MinuteOfHour = 30
			}
			input := ExecutionScheduleInput{ID: uuid.New(), ScenarioID: parent, ScenarioIterationID: iteration, Calendar: &spec, StartLocal: test.anchor, Policy: workers.CatchUp, MaxAttempts: 3, RequestBody: []byte(`{}`), Active: true}
			if err := store.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
				got, err := r.Schedules.Create(ctx, input)
				if err == nil && (got.NextLocal != test.anchor || got.Calendar == nil || got.Calendar.Timezone != test.zone || got.Interval != 0) {
					t.Error(got)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// The immutable definition is copied; caller mutation cannot retarget it.
			spec.Timezone = "UTC"
			if i == 0 {
				if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_calendar_failure BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='calendar queue unavailable'"); err != nil {
					t.Fatal(err)
				}
				err := store.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error { _, _ = r.Schedules.Materialize(ctx, input.ID, 3); return nil })
				if _, dropErr := f.db.Exec("DROP TRIGGER " + schema + ".fixture_calendar_failure"); dropErr != nil {
					t.Fatal(dropErr)
				}
				if err == nil || executionCount(t, f, "decision_engine_scheduled_executions") != 0 || executionCount(t, f, "decision_engine_schedule_occurrences") != 0 {
					t.Fatal("calendar wrote partial work", err)
				}
				if err := store.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
					got, err := r.Schedules.Get(ctx, input.ID)
					if err == nil && (got.NextLocal != test.anchor || got.WatermarkVersion != 0 || got.SkippedCount != 0) {
						t.Error("calendar cursor advanced without jobs", got)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			var materialized MaterializedExecutions
			if err := store.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error {
				var err error
				materialized, err = r.Schedules.Materialize(ctx, input.ID, 3)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if materialized.NextLocal != test.next || materialized.Skipped != test.skipped || len(materialized.Records) != 3 || !materialized.MoreDue {
				t.Fatal(materialized)
			}
			for j, record := range materialized.Records {
				if record.ScheduledFor.Format(time.RFC3339) != test.times[j] || record.TenantID != tenant.String() || record.ScenarioID != parent.String() {
					t.Fatal(record)
				}
			}
			expectedRecords += 3
			if executionCount(t, f, "decision_engine_schedule_occurrences") != expectedRecords || executionCount(t, f, "decision_engine_execution_job_deliveries") != expectedRecords {
				t.Fatal("calendar owner/job/receipt mismatch")
			}
			foreign := executionTestStore(t, f, uuid.New())
			if err := foreign.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error { _, err := r.Schedules.Materialize(ctx, input.ID, 1); return err }); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("foreign calendar materialized", err)
			}
			if err := foreign.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
				x := input
				x.ID = uuid.New()
				_, err := r.Schedules.Create(ctx, x)
				return err
			}); err == nil {
				t.Fatal("foreign scenario bound to calendar")
			}
			if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_schedules SET calendar_spec=JSON_SET(calendar_spec,'$.timezone','Pacific/Apia') WHERE id=?", mysqlstore.UUID(input.ID)); err == nil {
				t.Fatal("mutated immutable calendar")
			}
			if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_schedules SET next_local=start_local WHERE id=?", mysqlstore.UUID(input.ID)); err == nil {
				t.Fatal("regressed civil cursor")
			}
			if i == 3 {
				var wg sync.WaitGroup
				errs := make(chan error, 2)
				for n := 0; n < 2; n++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						errs <- store.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error { _, err := r.Schedules.Materialize(ctx, input.ID, 1); return err })
					}()
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatal(err)
					}
				}
				expectedRecords += 2
				if executionCount(t, f, "decision_engine_scheduled_executions") != expectedRecords || executionCount(t, f, "decision_engine_schedule_occurrences") != expectedRecords {
					t.Fatal("concurrent calendar ticks lost/duplicated occurrences")
				}
			}
			if err := store.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
				got, err := r.Schedules.Get(ctx, input.ID)
				if err != nil {
					return err
				}
				return r.Schedules.SetActive(ctx, input.ID, got.WatermarkVersion, false)
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	// An initial gap is accounted without inventing a physical occurrence.
	spec := recurrence.Spec{Version: 1, Frequency: "daily", Timezone: "America/New_York", TimeOfDay: "02:30", GapPolicy: recurrence.SkipGap, OverlapPolicy: recurrence.FirstOverlap}
	input := ExecutionScheduleInput{ID: uuid.New(), ScenarioID: parent, ScenarioIterationID: iteration, Calendar: &spec, StartLocal: "2020-03-08T02:30", Policy: workers.CatchUp, MaxAttempts: 3, RequestBody: []byte(`{}`), Active: true}
	if err := store.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
		got, err := r.Schedules.Create(ctx, input)
		if err == nil && (got.StartLocal != input.StartLocal || got.NextLocal != "2020-03-09T02:30" || got.SkippedCount != 1 || got.StartAt.Format(time.RFC3339) != "2020-03-09T06:30:00Z") {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ExecutionScheduleInput){func(v *ExecutionScheduleInput) { v.Interval = time.Hour }, func(v *ExecutionScheduleInput) { v.StartAt = time.Now().UTC() }, func(v *ExecutionScheduleInput) { v.StartLocal = "2020-03-08T02:31" }, func(v *ExecutionScheduleInput) { v.Policy = workers.SkipLate; v.LatenessWindow = 24 * time.Hour }} {
		x := input
		x.ID = uuid.New()
		x.Active = false // Do not let one-active-schedule uniqueness mask validation.
		change(&x)
		if err := store.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error { _, err := r.Schedules.Create(ctx, x); return err }); err == nil {
			t.Fatal("invalid mixed/calendar definition accepted")
		}
	}
}
