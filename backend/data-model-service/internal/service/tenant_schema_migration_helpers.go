package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

func recordTenantSchemaMigration(
	ctx context.Context,
	repository ports.TenantSchemaMigrationRepository,
	idGenerator ports.IDGenerator,
	tenantID uuid.UUID,
	version string,
	appliedAt time.Time,
) error {
	if repository == nil || idGenerator == nil {
		return fmt.Errorf("tenant schema migration repository and ID generator required")
	}
	if err := repository.Create(ctx, datamodel.TenantSchemaMigration{
		ID:        idGenerator.New(),
		TenantID:  tenantID,
		Version:   version,
		AppliedAt: appliedAt,
	}); err != nil {
		return fmt.Errorf("record tenant schema migration: %w", err)
	}
	return nil
}

func schemaMigrationVersion(operation, resourceType string) string {
	return fmt.Sprintf("%s:%s", operation, resourceType)
}
