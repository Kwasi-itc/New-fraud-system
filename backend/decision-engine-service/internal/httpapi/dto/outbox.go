package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
)

type OutboxEventResponse struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
}

func AdaptOutboxEvent(item integration.OutboxEvent) OutboxEventResponse {
	return OutboxEventResponse{
		ID:            item.ID,
		TenantID:      item.TenantID,
		AggregateType: item.AggregateType,
		AggregateID:   item.AggregateID,
		EventType:     item.EventType,
		Payload:       item.Payload,
		Status:        string(item.Status),
		CreatedAt:     item.CreatedAt,
	}
}
