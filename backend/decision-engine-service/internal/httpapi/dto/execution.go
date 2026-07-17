package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type CreateScheduledExecutionRequest struct {
	ScheduledFor   time.Time                 `json:"scheduled_for"`
	IdempotencyKey string                    `json:"idempotency_key"`
	Items          []EvaluateDecisionRequest `json:"items"`
	CandidateLimit int                       `json:"candidate_limit"`
}

type RecurringScheduleRequest struct {
	Enabled        bool   `json:"enabled"`
	Frequency      string `json:"frequency"`
	TimeOfDay      string `json:"time_of_day"`
	MinuteOfHour   int    `json:"minute_of_hour"`
	DayOfWeek      string `json:"day_of_week"`
	DayOfMonth     int    `json:"day_of_month"`
	Timezone       string `json:"timezone"`
	CandidateLimit int    `json:"candidate_limit"`
}

type RecurringScheduleResponse struct {
	Enabled        bool       `json:"enabled"`
	Frequency      string     `json:"frequency"`
	TimeOfDay      string     `json:"time_of_day"`
	MinuteOfHour   int        `json:"minute_of_hour"`
	DayOfWeek      string     `json:"day_of_week"`
	DayOfMonth     int        `json:"day_of_month"`
	Timezone       string     `json:"timezone"`
	CandidateLimit int        `json:"candidate_limit"`
	NextRun        *time.Time `json:"next_run,omitempty"`
	LastRun        *time.Time `json:"last_run,omitempty"`
}

type ScheduledExecutionResponse struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"tenant_id"`
	ScenarioID          string          `json:"scenario_id"`
	ScenarioIterationID string          `json:"scenario_iteration_id"`
	Source              string          `json:"source"`
	Status              string          `json:"status"`
	IdempotencyKey      string          `json:"idempotency_key,omitempty"`
	AttemptCount        int             `json:"attempt_count"`
	MaxAttempts         int             `json:"max_attempts"`
	ScheduledFor        time.Time       `json:"scheduled_for"`
	NextAttemptAt       *time.Time      `json:"next_attempt_at,omitempty"`
	RequestBody         json.RawMessage `json:"request_body"`
	LastError           string          `json:"last_error"`
	CreatedAt           time.Time       `json:"created_at"`
	FailedAt            *time.Time      `json:"failed_at,omitempty"`
}

type CreateAsyncDecisionExecutionRequest struct {
	ScenarioID     string                    `json:"scenario_id"`
	ObjectType     string                    `json:"object_type"`
	IdempotencyKey string                    `json:"idempotency_key"`
	WaitTimeoutMS  int                       `json:"wait_timeout_ms"`
	CallbackURL    string                    `json:"callback_url"`
	Items          []EvaluateDecisionRequest `json:"items"`
}

type AsyncDecisionExecutionResponse struct {
	ID                   string          `json:"id"`
	TenantID             string          `json:"tenant_id"`
	ScenarioID           string          `json:"scenario_id"`
	ObjectType           string          `json:"object_type"`
	Status               string          `json:"status"`
	IdempotencyKey       string          `json:"idempotency_key,omitempty"`
	AttemptCount         int             `json:"attempt_count"`
	MaxAttempts          int             `json:"max_attempts"`
	NextAttemptAt        *time.Time      `json:"next_attempt_at,omitempty"`
	RequestBody          json.RawMessage `json:"request_body"`
	ResultBody           json.RawMessage `json:"result_body,omitempty"`
	CallbackURL          string          `json:"callback_url,omitempty"`
	CallbackStatus       string          `json:"callback_status,omitempty"`
	CallbackAttemptCount int             `json:"callback_attempt_count"`
	CallbackLastError    string          `json:"callback_last_error,omitempty"`
	CallbackSentAt       *time.Time      `json:"callback_sent_at,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	CompletedAt          *time.Time      `json:"completed_at,omitempty"`
	FailedAt             *time.Time      `json:"failed_at,omitempty"`
}

type CreateAsyncDecisionExecutionResponse struct {
	AsyncDecisionExecution AsyncDecisionExecutionResponse `json:"async_decision_execution"`
	CompletedInline        bool                           `json:"completed_inline"`
}

type ExecutionStatusSummaryResponse struct {
	Pending   int `json:"pending"`
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

func AdaptScheduledExecution(item execution.ScheduledExecution) ScheduledExecutionResponse {
	return ScheduledExecutionResponse{
		ID:                  item.ID,
		TenantID:            item.TenantID,
		ScenarioID:          item.ScenarioID,
		ScenarioIterationID: item.ScenarioIterationID,
		Source:              string(item.Source),
		Status:              string(item.Status),
		IdempotencyKey:      item.IdempotencyKey,
		AttemptCount:        item.AttemptCount,
		MaxAttempts:         item.MaxAttempts,
		ScheduledFor:        item.ScheduledFor,
		NextAttemptAt:       item.NextAttemptAt,
		RequestBody:         item.RequestBody,
		LastError:           item.LastError,
		CreatedAt:           item.CreatedAt,
		FailedAt:            item.FailedAt,
	}
}

func AdaptAsyncDecisionExecution(item execution.AsyncDecisionExecution) AsyncDecisionExecutionResponse {
	return AsyncDecisionExecutionResponse{
		ID:                   item.ID,
		TenantID:             item.TenantID,
		ScenarioID:           item.ScenarioID,
		ObjectType:           item.ObjectType,
		Status:               string(item.Status),
		IdempotencyKey:       item.IdempotencyKey,
		AttemptCount:         item.AttemptCount,
		MaxAttempts:          item.MaxAttempts,
		NextAttemptAt:        item.NextAttemptAt,
		RequestBody:          item.RequestBody,
		ResultBody:           item.ResultBody,
		CallbackURL:          item.CallbackURL,
		CallbackStatus:       item.CallbackStatus,
		CallbackAttemptCount: item.CallbackAttemptCount,
		CallbackLastError:    item.CallbackLastError,
		CallbackSentAt:       item.CallbackSentAt,
		CreatedAt:            item.CreatedAt,
		CompletedAt:          item.CompletedAt,
		FailedAt:             item.FailedAt,
	}
}

func AdaptExecutionStatusSummary(item service.ExecutionStatusSummary) ExecutionStatusSummaryResponse {
	return ExecutionStatusSummaryResponse{
		Pending:   item.Pending,
		Queued:    item.Queued,
		Running:   item.Running,
		Completed: item.Completed,
		Failed:    item.Failed,
	}
}

func AdaptAsyncExecutionRequest(req CreateAsyncDecisionExecutionRequest) service.AsyncDecisionExecutionRequest {
	items := make([]service.DecisionEvaluationRequest, len(req.Items))
	for i, item := range req.Items {
		items[i] = service.DecisionEvaluationRequest{
			ObjectID:   item.ObjectID,
			ObjectType: item.ObjectType,
			Fields:     item.Fields,
		}
	}
	return service.AsyncDecisionExecutionRequest{
		ScenarioID:     req.ScenarioID,
		ObjectType:     req.ObjectType,
		IdempotencyKey: req.IdempotencyKey,
		WaitTimeoutMS:  req.WaitTimeoutMS,
		CallbackURL:    req.CallbackURL,
		Items:          items,
	}
}

func AdaptScheduledExecutionRequest(req CreateScheduledExecutionRequest) service.ScheduledExecutionRequest {
	items := make([]service.DecisionEvaluationRequest, len(req.Items))
	for i, item := range req.Items {
		items[i] = service.DecisionEvaluationRequest{
			ObjectID:   item.ObjectID,
			ObjectType: item.ObjectType,
			Fields:     item.Fields,
		}
	}
	return service.ScheduledExecutionRequest{Items: items, CandidateLimit: req.CandidateLimit, IdempotencyKey: req.IdempotencyKey}
}

func AdaptRecurringScheduleRequest(req RecurringScheduleRequest) service.RecurringScheduleConfig {
	return service.RecurringScheduleConfig{
		Enabled:        req.Enabled,
		Frequency:      req.Frequency,
		TimeOfDay:      req.TimeOfDay,
		MinuteOfHour:   req.MinuteOfHour,
		DayOfWeek:      req.DayOfWeek,
		DayOfMonth:     req.DayOfMonth,
		Timezone:       req.Timezone,
		CandidateLimit: req.CandidateLimit,
	}
}

func AdaptRecurringSchedule(item service.RecurringScheduleConfig) RecurringScheduleResponse {
	return RecurringScheduleResponse{
		Enabled:        item.Enabled,
		Frequency:      item.Frequency,
		TimeOfDay:      item.TimeOfDay,
		MinuteOfHour:   item.MinuteOfHour,
		DayOfWeek:      item.DayOfWeek,
		DayOfMonth:     item.DayOfMonth,
		Timezone:       item.Timezone,
		CandidateLimit: item.CandidateLimit,
		NextRun:        item.NextRun,
		LastRun:        item.LastRun,
	}
}
