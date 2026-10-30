package mysqlapi

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/recurrence"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

// Administration returns definition/watermark data, never internal request JSON.
// Disabled definitions remain addressable by their immutable schedule ID.
type ScheduleResponse struct {
	ID               uuid.UUID               `json:"id"`
	ScenarioID       uuid.UUID               `json:"scenario_id"`
	IterationID      uuid.UUID               `json:"scenario_iteration_id"`
	Calendar         *recurrence.Spec        `json:"calendar,omitempty"`
	StartLocal       string                  `json:"start_local,omitempty"`
	IntervalUS       int64                   `json:"interval_us,omitempty"`
	StartAt          time.Time               `json:"start_at"`
	NextAt           time.Time               `json:"next_at"`
	Active           bool                    `json:"active"`
	WatermarkVersion int64                   `json:"watermark_version"`
	SkippedCount     int64                   `json:"skipped_count"`
	MissedPolicy     workers.MissedRunPolicy `json:"missed_policy"`
	LatenessMS       int64                   `json:"lateness_ms"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

func adaptSchedule(v mysql.ExecutionScheduleRecord) ScheduleResponse {
	return ScheduleResponse{ID: v.ID, ScenarioID: v.ScenarioID, IterationID: v.ScenarioIterationID, Calendar: v.Calendar, StartLocal: v.StartLocal, IntervalUS: v.Interval.Microseconds(), StartAt: v.StartAt, NextAt: v.NextAt, Active: v.Active, WatermarkVersion: v.WatermarkVersion, SkippedCount: v.SkippedCount, MissedPolicy: v.Policy, LatenessMS: v.LatenessWindow.Milliseconds(), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

type DisableScheduleRequest struct {
	ExpectedScheduleID uuid.UUID `json:"expected_schedule_id"`
	ExpectedWatermark  *int64    `json:"expected_watermark_version"`
}

func (a *ExecutionAdapter) Schedule(ctx context.Context, parent, id uuid.UUID) (out *ScheduleResponse, err error) {
	if a == nil || a.store == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		var v *mysql.ExecutionScheduleRecord
		var err error
		if id == uuid.Nil {
			v, err = r.Schedules.Active(ctx, parent)
		} else {
			var record mysql.ExecutionScheduleRecord
			record, err = r.Schedules.Get(ctx, id)
			if err == nil && record.ScenarioID != parent {
				return mysql.ErrTenantScope
			}
			v = &record
		}
		if err != nil {
			return err
		}
		if v != nil {
			x := adaptSchedule(*v)
			out = &x
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (a *ExecutionAdapter) DisableSchedule(ctx context.Context, parent uuid.UUID, req DisableScheduleRequest) (ScheduleResponse, error) {
	if a == nil || a.store == nil {
		return ScheduleResponse{}, mysql.ErrDecisionAuthorizationUnavailable
	}
	if req.ExpectedScheduleID == uuid.Nil || req.ExpectedWatermark == nil || *req.ExpectedWatermark < 0 {
		return ScheduleResponse{}, ErrCancellationRequest
	}
	v, err := a.store.DisableSchedule(ctx, parent, req.ExpectedScheduleID, *req.ExpectedWatermark)
	if err != nil {
		return ScheduleResponse{}, err
	}
	return adaptSchedule(v), nil
}
