package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
)

type CreateScreeningConfigRequest struct {
	Name            string          `json:"name"`
	AllowedOutcomes []string        `json:"allowed_outcomes"`
	Provider        string          `json:"provider"`
	ConfigJSON      json.RawMessage `json:"config_json"`
	Active          bool            `json:"active"`
}

type UpdateScreeningConfigRequest struct {
	Name            string          `json:"name"`
	AllowedOutcomes []string        `json:"allowed_outcomes"`
	Provider        string          `json:"provider"`
	ConfigJSON      json.RawMessage `json:"config_json"`
	Active          bool            `json:"active"`
}

type ScreeningConfigResponse struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	ScenarioID      string          `json:"scenario_id"`
	Name            string          `json:"name"`
	AllowedOutcomes []string        `json:"allowed_outcomes"`
	Provider        string          `json:"provider"`
	ConfigJSON      json.RawMessage `json:"config_json"`
	Active          bool            `json:"active"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ScreeningExecutionResponse struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	ConfigID          string          `json:"config_id"`
	DecisionID        string          `json:"decision_id"`
	ScenarioID        string          `json:"scenario_id"`
	Status            string          `json:"status"`
	RequestJSON       json.RawMessage `json:"request_json"`
	ResponseJSON      json.RawMessage `json:"response_json"`
	ProviderReference string          `json:"provider_reference"`
	LastError         string          `json:"last_error"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	SentAt            *time.Time      `json:"sent_at,omitempty"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	FailedAt          *time.Time      `json:"failed_at,omitempty"`
}

type UpdateScreeningExecutionStatusRequest struct {
	Status            string           `json:"status"`
	ProviderReference *string          `json:"provider_reference"`
	ResponseJSON      *json.RawMessage `json:"response_json"`
	LastError         *string          `json:"last_error"`
}

func AdaptScreeningConfig(item screening.Config) ScreeningConfigResponse {
	return ScreeningConfigResponse{
		ID:              item.ID,
		TenantID:        item.TenantID,
		ScenarioID:      item.ScenarioID,
		Name:            item.Name,
		AllowedOutcomes: item.AllowedOutcomes,
		Provider:        item.Provider,
		ConfigJSON:      item.ConfigJSON,
		Active:          item.Active,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func AdaptScreeningExecution(item screening.Execution) ScreeningExecutionResponse {
	return ScreeningExecutionResponse{
		ID:                item.ID,
		TenantID:          item.TenantID,
		ConfigID:          item.ConfigID,
		DecisionID:        item.DecisionID,
		ScenarioID:        item.ScenarioID,
		Status:            string(item.Status),
		RequestJSON:       item.RequestJSON,
		ResponseJSON:      item.ResponseJSON,
		ProviderReference: item.ProviderReference,
		LastError:         item.LastError,
		CreatedAt:         item.CreatedAt,
		UpdatedAt:         item.UpdatedAt,
		SentAt:            item.SentAt,
		CompletedAt:       item.CompletedAt,
		FailedAt:          item.FailedAt,
	}
}
