package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
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

type TenantSchemaMigrationResponse struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Version   string    `json:"version"`
	AppliedAt time.Time `json:"applied_at"`
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

func AdaptTenantSchemaMigration(migration datamodel.TenantSchemaMigration) TenantSchemaMigrationResponse {
	return TenantSchemaMigrationResponse{
		ID:        migration.ID,
		TenantID:  migration.TenantID,
		Version:   migration.Version,
		AppliedAt: migration.AppliedAt,
	}
}
