package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type EventSchemaService struct {
	repository ports.EventSchemaRepository
	clock      ports.Clock
}

func NewEventSchemaService(repository ports.EventSchemaRepository, clock ports.Clock) EventSchemaService {
	return EventSchemaService{repository: repository, clock: clock}
}

func (s EventSchemaService) Lock(ctx context.Context, tenantID, tableID uuid.UUID, schemaRevision string) (datamodel.Table, error) {
	schemaRevision = strings.TrimSpace(schemaRevision)
	if schemaRevision == "" {
		return datamodel.Table{}, fmt.Errorf("schema_revision is required")
	}
	return s.repository.Lock(ctx, tenantID, tableID, schemaRevision, s.clock.Now())
}
