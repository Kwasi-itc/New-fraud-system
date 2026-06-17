package dto

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type CreateScheduledExecutionRequest struct {
	ScheduledFor   time.Time                 `json:"scheduled_for"`
	Items          []EvaluateDecisionRequest `json:"items"`
	CandidateLimit int                       `json:"candidate_limit"`
}

type RecurringScheduleRequest struct {
	Enabled        bool   `json:"enabled"`
	Frequency      string `json:"frequency"`
	TimeOfDay      string `json:"time_of_day"`
	Timezone       string `json:"timezone"`
	CandidateLimit int    `json:"candidate_limit"`
}

type RecurringScheduleResponse struct {
	Enabled        bool   `json:"enabled"`
	Frequency      string `json:"frequency"`
	TimeOfDay      string `json:"time_of_day"`
	Timezone       string `json:"timezone"`
	CandidateLimit int    `json:"candidate_limit"`
}

type ScheduledExecutionResponse struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenant_id"`
	ScenarioID          string    `json:"scenario_id"`
	ScenarioIterationID string    `json:"scenario_iteration_id"`
	Status              string    `json:"status"`
	ScheduledFor        time.Time `json:"scheduled_for"`
	RequestBody         []byte    `json:"request_body"`
	CreatedAt           time.Time `json:"created_at"`
}

type CreateAsyncDecisionExecutionRequest struct {
	ScenarioID string                    `json:"scenario_id"`
	ObjectType string                    `json:"object_type"`
	Items      []EvaluateDecisionRequest `json:"items"`
}

type AsyncDecisionExecutionResponse struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	ScenarioID  string    `json:"scenario_id"`
	ObjectType  string    `json:"object_type"`
	Status      string    `json:"status"`
	RequestBody []byte    `json:"request_body"`
	CreatedAt   time.Time `json:"created_at"`
}

func AdaptScheduledExecution(item execution.ScheduledExecution) ScheduledExecutionResponse {
	return ScheduledExecutionResponse{
		ID:                  item.ID,
		TenantID:            item.TenantID,
		ScenarioID:          item.ScenarioID,
		ScenarioIterationID: item.ScenarioIterationID,
		Status:              string(item.Status),
		ScheduledFor:        item.ScheduledFor,
		RequestBody:         item.RequestBody,
		CreatedAt:           item.CreatedAt,
	}
}

func AdaptAsyncDecisionExecution(item execution.AsyncDecisionExecution) AsyncDecisionExecutionResponse {
	return AsyncDecisionExecutionResponse{
		ID:          item.ID,
		TenantID:    item.TenantID,
		ScenarioID:  item.ScenarioID,
		ObjectType:  item.ObjectType,
		Status:      string(item.Status),
		RequestBody: item.RequestBody,
		CreatedAt:   item.CreatedAt,
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
		ScenarioID: req.ScenarioID,
		ObjectType: req.ObjectType,
		Items:      items,
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
	return service.ScheduledExecutionRequest{Items: items, CandidateLimit: req.CandidateLimit}
}

func AdaptRecurringScheduleRequest(req RecurringScheduleRequest) service.RecurringScheduleConfig {
	return service.RecurringScheduleConfig{
		Enabled:        req.Enabled,
		Frequency:      req.Frequency,
		TimeOfDay:      req.TimeOfDay,
		Timezone:       req.Timezone,
		CandidateLimit: req.CandidateLimit,
	}
}

func AdaptRecurringSchedule(item service.RecurringScheduleConfig) RecurringScheduleResponse {
	return RecurringScheduleResponse{
		Enabled:        item.Enabled,
		Frequency:      item.Frequency,
		TimeOfDay:      item.TimeOfDay,
		Timezone:       item.Timezone,
		CandidateLimit: item.CandidateLimit,
	}
}
