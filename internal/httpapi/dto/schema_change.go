package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
)

type SchemaChangeResponse struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	Operation    string    `json:"operation"`
	ResourceType string    `json:"resource_type"`
	ResourceID   uuid.UUID `json:"resource_id"`
	Status       string    `json:"status"`
	Details      []byte    `json:"details"`
	CreatedAt    time.Time `json:"created_at"`
}

func AdaptSchemaChange(change datamodel.SchemaChange) SchemaChangeResponse {
	return SchemaChangeResponse{
		ID:           change.ID,
		TenantID:     change.TenantID,
		Operation:    change.Operation,
		ResourceType: change.ResourceType,
		ResourceID:   change.ResourceID,
		Status:       change.Status,
		Details:      change.Details,
		CreatedAt:    change.CreatedAt,
	}
}

