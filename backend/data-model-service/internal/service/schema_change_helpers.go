package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

func newSchemaChange(
	id uuid.UUID,
	tenantID uuid.UUID,
	operation string,
	resourceType string,
	resourceID uuid.UUID,
	createdAt time.Time,
	details map[string]any,
) datamodel.SchemaChange {
	payload, err := json.Marshal(details)
	if err != nil {
		payload = []byte(`{}`)
	}
	return datamodel.SchemaChange{
		ID:           id,
		TenantID:     tenantID,
		Operation:    operation,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Status:       "applied",
		Details:      payload,
		CreatedAt:    createdAt,
	}
}
