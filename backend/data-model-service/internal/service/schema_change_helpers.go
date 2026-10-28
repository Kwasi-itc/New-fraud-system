package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

func recordSchemaChange(
	ctx context.Context,
	repository ports.SchemaChangeRepository,
	id uuid.UUID,
	tenantID uuid.UUID,
	operation string,
	resourceType string,
	resourceID uuid.UUID,
	createdAt time.Time,
	details map[string]any,
) error {
	if repository == nil {
		return fmt.Errorf("schema-change repository required")
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode schema change: %w", err)
	}
	if err := repository.Create(ctx, datamodel.SchemaChange{
		ID:           id,
		TenantID:     tenantID,
		Operation:    operation,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Status:       "applied",
		Details:      payload,
		CreatedAt:    createdAt,
	}); err != nil {
		return fmt.Errorf("record schema change: %w", err)
	}
	return nil
}
