package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type CreateTenantRequest struct {
	Name        string  `json:"name" binding:"required"`
	ExternalKey *string `json:"external_key"`
}

type TenantResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ExternalKey *string   `json:"external_key,omitempty"`
	SchemaName  string    `json:"schema_name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func AdaptTenant(record tenant.Tenant) TenantResponse {
	return TenantResponse{
		ID:          record.ID,
		Name:        record.Name,
		ExternalKey: record.ExternalKey,
		SchemaName:  record.SchemaName,
		Status:      string(record.Status),
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}
