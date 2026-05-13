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
) {
	if repository == nil {
		return
	}
	_ = repository.Create(ctx, datamodel.TenantSchemaMigration{
		ID:        idGenerator.New(),
		TenantID:  tenantID,
		Version:   version,
		AppliedAt: appliedAt,
	})
}

func schemaMigrationVersion(operation, resourceType string) string {
	return fmt.Sprintf("%s:%s", operation, resourceType)
}
