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
	ScenarioID          string `json:"scenario_id"`
	IterationID         string `json:"iteration_id"`
	PreparationRequired bool   `json:"preparation_required"`
	PreparationStarted  bool   `json:"preparation_started"`
	PreparationFinished bool   `json:"preparation_finished"`
	PendingItems        int    `json:"pending_items"`
}

func AdaptPublicationPreparationStatus(s scenario.PublicationPreparationStatus) PublicationPreparationStatusResponse {
	return PublicationPreparationStatusResponse{
		ScenarioID:          s.ScenarioID,
		IterationID:         s.IterationID,
		PreparationRequired: s.PreparationRequired,
		PreparationStarted:  s.PreparationStarted,
		PreparationFinished: s.PreparationFinished,
		PendingItems:        s.PendingItems,
	}
}
