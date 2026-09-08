package dto

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type PublicationActionRequest struct {
	Action      string `json:"action"`
	IterationID string `json:"iteration_id"`
}

type PublicationResponse struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	ScenarioID  string    `json:"scenario_id"`
	IterationID string    `json:"iteration_id"`
	Action      string    `json:"action"`
	CreatedAt   time.Time `json:"created_at"`
}

func AdaptPublication(p scenario.Publication) PublicationResponse {
	return PublicationResponse{
		ID:          p.ID,
		TenantID:    p.TenantID,
		ScenarioID:  p.ScenarioID,
		IterationID: p.IterationID,
		Action:      string(p.Action),
		CreatedAt:   p.CreatedAt,
	}
}

type PublicationPreparationStatusResponse struct {
	ScenarioID              string                           `json:"scenario_id"`
	IterationID             string                           `json:"iteration_id"`
	PreparationRequired     bool                             `json:"preparation_required"`
	PreparationStarted      bool                             `json:"preparation_started"`
	PreparationFinished     bool                             `json:"preparation_finished"`
	PendingItems            int                              `json:"pending_items"`
	DistributionSuggestions []DistributionSuggestionResponse `json:"distribution_suggestions,omitempty"`
}

type DistributionSuggestionResponse struct {
	TableName             string  `json:"table_name"`
	FieldName             string  `json:"field_name"`
	AcceptedCategory      string  `json:"accepted_category"`
	SuggestedCategory     string  `json:"suggested_category"`
	Reason                string  `json:"reason"`
	RowsAnalyzed          int64   `json:"rows_analyzed"`
	NonNullRows           int64   `json:"non_null_rows"`
	DistinctValues        int64   `json:"distinct_values"`
	ExpectedSameValueRows float64 `json:"expected_same_value_rows"`
	PolicyVersion         string  `json:"policy_version"`
}

func AdaptPublicationPreparationStatus(s scenario.PublicationPreparationStatus) PublicationPreparationStatusResponse {
	response := PublicationPreparationStatusResponse{
		ScenarioID:          s.ScenarioID,
		IterationID:         s.IterationID,
		PreparationRequired: s.PreparationRequired,
		PreparationStarted:  s.PreparationStarted,
		PreparationFinished: s.PreparationFinished,
		PendingItems:        s.PendingItems,
	}
	for _, suggestion := range s.DistributionSuggestions {
		response.DistributionSuggestions = append(response.DistributionSuggestions, DistributionSuggestionResponse{
			TableName: suggestion.TableName, FieldName: suggestion.FieldName,
			AcceptedCategory: suggestion.AcceptedCategory, SuggestedCategory: suggestion.SuggestedCategory,
			Reason: suggestion.Reason, RowsAnalyzed: suggestion.RowsAnalyzed, NonNullRows: suggestion.NonNullRows,
			DistinctValues: suggestion.DistinctValues, ExpectedSameValueRows: suggestion.ExpectedSameValueRows,
			PolicyVersion: suggestion.PolicyVersion,
		})
	}
	return response
}
