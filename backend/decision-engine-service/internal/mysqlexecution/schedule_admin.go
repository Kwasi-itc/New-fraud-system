package mysqlexecution

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/recurrence"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

type CalendarScheduleRequest struct {
	ScheduleID         uuid.UUID               `json:"schedule_id"`
	ExpectedScheduleID uuid.UUID               `json:"expected_schedule_id"`
	ExpectedWatermark  *int64                  `json:"expected_watermark_version"`
	Calendar           recurrence.Spec         `json:"calendar"`
	StartLocal         string                  `json:"start_local"`
	MissedPolicy       workers.MissedRunPolicy `json:"missed_policy"`
	LatenessMS         int64                   `json:"lateness_ms"`
	CandidateLimit     int                     `json:"candidate_limit"`
}

// ScheduleAdmin prepares outside locks, then delegates the atomic old/new
// definition switch to the owner. Every occurrence remains independently fenced.
type ScheduleAdmin struct {
	preparer    *Preparer
	maxAttempts int
}

func NewScheduleAdmin(p *Preparer, maxAttempts int) (*ScheduleAdmin, error) {
	if p == nil || maxAttempts < 1 || maxAttempts > 2147483647 {
		return nil, service.ErrExecutionPreparation
	}
	return &ScheduleAdmin{p, maxAttempts}, nil
}
func (s *ScheduleAdmin) MatchesExecutionStore(owner *mysql.ExecutionStore) bool {
	return s != nil && s.preparer != nil && s.preparer.executions == owner
}

func (s *ScheduleAdmin) Replace(ctx context.Context, parent uuid.UUID, request CalendarScheduleRequest) (mysql.ExecutionScheduleRecord, error) {
	if s == nil || s.preparer == nil {
		return mysql.ExecutionScheduleRecord{}, mysql.ErrOwnerContractUnavailable
	}
	p := s.preparer
	bad := service.ErrExecutionPreparation
	if parent == uuid.Nil || request.ScheduleID == uuid.Nil || request.ExpectedWatermark == nil || *request.ExpectedWatermark < 0 || request.ExpectedScheduleID == uuid.Nil && *request.ExpectedWatermark != 0 || request.CandidateLimit < 1 || request.CandidateLimit > p.limits.MaxItems || request.LatenessMS < 0 || request.LatenessMS > int64((24*time.Hour)/time.Millisecond) {
		return mysql.ExecutionScheduleRecord{}, bad
	}
	c, err := recurrence.New(request.Calendar)
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, errors.Join(bad, err)
	}
	late := time.Duration(request.LatenessMS) * time.Millisecond
	if err := c.ValidatePolicy(request.MissedPolicy, late); err != nil {
		return mysql.ExecutionScheduleRecord{}, errors.Join(bad, err)
	}
	anchor, err := c.ParseSlot(request.StartLocal)
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, errors.Join(bad, err)
	}
	if _, _, _, err := c.First(anchor); err != nil {
		return mysql.ExecutionScheduleRecord{}, errors.Join(bad, err)
	}
	snapshot, err := p.executions.PreparationForScheduleAdmin(ctx, parent)
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, err
	}
	pin, err := service.NewPinnedScenarioSnapshot(snapshot.Scenario(), snapshot.Iteration(), snapshot.Rules(), p.metadataLimits)
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, err
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, err
	}
	if err := service.ValidatePinnedModel(pin, model); err != nil {
		return mysql.ExecutionScheduleRecord{}, err
	}
	body, err := json.Marshal(service.ScheduledExecutionRequest{CandidateLimit: request.CandidateLimit})
	if err != nil {
		return mysql.ExecutionScheduleRecord{}, err
	}
	v := mysql.ExecutionScheduleInput{ID: request.ScheduleID, ScenarioID: parent, ScenarioIterationID: uuid.MustParse(pin.IterationID()), Calendar: &request.Calendar, StartLocal: request.StartLocal, Policy: request.MissedPolicy, LatenessWindow: late, MaxAttempts: s.maxAttempts, RequestBody: body, Active: true}
	return p.executions.ReplaceSchedule(ctx, p.reader, snapshot, model, v, request.ExpectedScheduleID, *request.ExpectedWatermark)
}
