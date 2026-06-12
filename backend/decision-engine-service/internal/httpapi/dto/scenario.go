package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type CreateScenarioRequest struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	TriggerObjectType string `json:"trigger_object_type"`
}

type UpdateScenarioRequest struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	TriggerObjectType string `json:"trigger_object_type"`
}

type CopyScenarioRequest struct {
	Name string `json:"name"`
}

type ScenarioResponse struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	TriggerObjectType string     `json:"trigger_object_type"`
	LiveIterationID   *string    `json:"live_iteration_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func AdaptScenario(s scenario.Scenario) ScenarioResponse {
	return ScenarioResponse{
		ID:                s.ID,
		TenantID:          s.TenantID,
		Name:              s.Name,
		Description:       s.Description,
		TriggerObjectType: s.TriggerObjectType,
		LiveIterationID:   s.LiveIterationID,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
	}
}

type IterationResponse struct {
	ID                           string          `json:"id"`
	ScenarioID                   string          `json:"scenario_id"`
	TenantID                     string          `json:"tenant_id"`
	Version                      int             `json:"version"`
	Status                       string          `json:"status"`
	TriggerFormula               json.RawMessage `json:"trigger_formula,omitempty"`
	ScoreReviewThreshold         *int            `json:"score_review_threshold,omitempty"`
	ScoreBlockAndReviewThreshold *int            `json:"score_block_and_review_threshold,omitempty"`
	ScoreDeclineThreshold        *int            `json:"score_decline_threshold,omitempty"`
	Schedule                     string          `json:"schedule,omitempty"`
	CreatedAt                    time.Time       `json:"created_at"`
	CommittedAt                  *time.Time      `json:"committed_at,omitempty"`
}

func AdaptIteration(i scenario.Iteration) IterationResponse {
	return IterationResponse{
		ID:                           i.ID,
		ScenarioID:                   i.ScenarioID,
		TenantID:                     i.TenantID,
		Version:                      i.Version,
		Status:                       string(i.Status),
		TriggerFormula:               i.TriggerFormula,
		ScoreReviewThreshold:         i.ScoreReviewThreshold,
		ScoreBlockAndReviewThreshold: i.ScoreBlockAndReviewThreshold,
		ScoreDeclineThreshold:        i.ScoreDeclineThreshold,
		Schedule:                     i.Schedule,
		CreatedAt:                    i.CreatedAt,
		CommittedAt:                  i.CommittedAt,
	}
}

type UpdateIterationRequest struct {
	TriggerFormula               json.RawMessage `json:"trigger_formula"`
	ScoreReviewThreshold         *int            `json:"score_review_threshold"`
	ScoreBlockAndReviewThreshold *int            `json:"score_block_and_review_threshold"`
	ScoreDeclineThreshold        *int            `json:"score_decline_threshold"`
	Schedule                     string          `json:"schedule"`
}

type MetadataIterationResponse struct {
	ID          string     `json:"id"`
	ScenarioID  string     `json:"scenario_id"`
	TenantID    string     `json:"tenant_id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CommittedAt *time.Time `json:"committed_at,omitempty"`
}

func AdaptIterationMetadata(i scenario.Iteration) MetadataIterationResponse {
	return MetadataIterationResponse{
		ID:          i.ID,
		ScenarioID:  i.ScenarioID,
		TenantID:    i.TenantID,
		Version:     i.Version,
		Status:      string(i.Status),
		CreatedAt:   i.CreatedAt,
		CommittedAt: i.CommittedAt,
	}
}

type NotImplementedResponse struct {
	Error   string `json:"error"`
	Details string `json:"details"`
}
