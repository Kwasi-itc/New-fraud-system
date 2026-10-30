package mysqlexecution

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/recurrence"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func TestMySQLScheduleAdministrationPreparationAtomicReplaceAndRevocation(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	admin, err := NewScheduleAdmin(f.preparer, 3)
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	request := CalendarScheduleRequest{ScheduleID: uuid.New(), ExpectedWatermark: &zero, Calendar: recurrence.Spec{Version: 1, Frequency: "monthly", Timezone: "UTC", TimeOfDay: "09:30", DayOfMonth: 31, GapPolicy: recurrence.SkipGap, OverlapPolicy: recurrence.FirstOverlap}, StartLocal: "2020-01-31T09:30", MissedPolicy: workers.CatchUp, CandidateLimit: 2}
	original, err := admin.Replace(ctx, f.parent, request)
	if err != nil || original.ID != request.ScheduleID || !original.Active {
		t.Fatal(original, err)
	}
	get := func(id uuid.UUID) mysql.ExecutionScheduleRecord {
		t.Helper()
		var out mysql.ExecutionScheduleRecord
		if err := f.executions.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
			var err error
			out, err = r.Schedules.Get(ctx, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	request.ExpectedScheduleID = original.ID
	request.ScheduleID = uuid.New()
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".fixture_schedule_fail BEFORE INSERT ON " + f.schema + ".decision_engine_execution_schedules FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='definition unavailable'"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Replace(ctx, f.parent, request); err == nil || !get(original.ID).Active {
		t.Fatal("replacement did not roll back", err)
	}
	if _, err := f.db.Exec("DROP TRIGGER " + f.schema + ".fixture_schedule_fail"); err != nil {
		t.Fatal(err)
	}
	// Two replacements of the same observed definition may not both commit.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := request
			req.ScheduleID = uuid.New()
			_, err := admin.Replace(ctx, f.parent, req)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, mysql.ErrExecutionState) {
			t.Fatal(err)
		}
	}
	if success != 1 || get(original.ID).Active {
		t.Fatal("concurrent replacement lost precondition", success)
	}
	var active *mysql.ExecutionScheduleRecord
	if err := f.executions.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		var err error
		active, err = r.Schedules.Active(ctx, f.parent)
		return err
	}); err != nil || active == nil {
		t.Fatal(active, err)
	}
	if _, err := f.executions.DisableSchedule(ctx, uuid.New(), active.ID, active.WatermarkVersion); !errors.Is(err, mysql.ErrExecutionState) {
		t.Fatal("foreign scenario disable", err)
	}
	if _, err := f.executions.DisableSchedule(ctx, f.parent, active.ID, active.WatermarkVersion+1); !errors.Is(err, mysql.ErrExecutionState) {
		t.Fatal("stale disable", err)
	}
	disabled, err := f.executions.DisableSchedule(ctx, f.parent, active.ID, active.WatermarkVersion)
	if err != nil || disabled.Active || disabled.WatermarkVersion != active.WatermarkVersion+1 {
		t.Fatal(disabled, err)
	}
	request.ExpectedScheduleID = uuid.Nil
	request.ScheduleID = uuid.New()
	if _, err := f.db.Exec("UPDATE " + f.schema + ".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Replace(ctx, f.parent, request); err == nil {
		t.Fatal("closed model scheduled")
	}
	f.revoked.Store(true)
	if _, err := admin.Replace(ctx, f.parent, request); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked administration", err)
	}
	if _, err := f.executions.DisableSchedule(ctx, f.parent, disabled.ID, disabled.WatermarkVersion); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked disable", err)
	}
}

func TestMySQLScheduleAdministrationFencesPublicationChange(t *testing.T) {
	f := setupPreparation(t)
	admin, err := NewScheduleAdmin(f.preparer, 3)
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	req := CalendarScheduleRequest{ScheduleID: uuid.New(), ExpectedWatermark: &zero, Calendar: recurrence.Spec{Version: 1, Frequency: "daily", Timezone: "UTC", TimeOfDay: "09:30", GapPolicy: recurrence.SkipGap, OverlapPolicy: recurrence.FirstOverlap}, StartLocal: "2026-10-31T09:30", MissedPolicy: workers.Coalesce, CandidateLimit: 2}
	f.provider.onRead = func() { f.publish(t, 50) }
	if _, err := admin.Replace(context.Background(), f.parent, req); !errors.Is(err, mysql.ErrExecutionBindingChanged) {
		t.Fatal("stale publication admitted", err)
	}
	if f.count(t, "decision_engine_execution_schedules") != 0 {
		t.Fatal("failed preparation wrote schedule")
	}
}

func TestMySQLScheduleReplacementAndTickUseCompatibleLockOrder(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	admin, err := NewScheduleAdmin(f.preparer, 3)
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	req := CalendarScheduleRequest{ScheduleID: uuid.New(), ExpectedWatermark: &zero, Calendar: recurrence.Spec{Version: 1, Frequency: "daily", Timezone: "UTC", TimeOfDay: "09:30", GapPolicy: recurrence.SkipGap, OverlapPolicy: recurrence.FirstOverlap}, StartLocal: "2020-01-01T09:30", MissedPolicy: workers.CatchUp, CandidateLimit: 2}
	original, err := admin.Replace(ctx, f.parent, req)
	if err != nil {
		t.Fatal(err)
	}
	req.ExpectedScheduleID = original.ID
	req.ScheduleID = uuid.New()
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; _, err := admin.Replace(ctx, f.parent, req); results <- err }()
	go func() {
		<-start
		results <- f.executions.Run(ctx, mysql.ExecutionScheduleTick, func(r mysql.ExecutionRepositories) error {
			_, err := r.Schedules.Materialize(ctx, original.ID, 1)
			return err
		})
	}()
	close(start)
	acknowledged := 0
	for range 2 {
		err := <-results
		if err == nil {
			acknowledged++
		} else if !errors.Is(err, mysql.ErrExecutionState) {
			t.Fatal("lock-order failure", err)
		}
	}
	if acknowledged != 1 {
		t.Fatal("stale watermark replacement or tick succeeded", acknowledged)
	}
}
