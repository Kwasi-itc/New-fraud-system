package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

func (m *PhysicalPublicationManager) PublishField(ctx context.Context, r ports.PhysicalPublicationRequest, id uuid.UUID, effects func(ports.MutationStore) error) (datamodel.Field, error) {
	if id == uuid.Nil {
		return datamodel.Field{}, errors.New("retained physical field identity required")
	}
	retained := &metadataResult{kind: fieldResultKind, id: id}
	if _, err := m.publish(ctx, r, effects, retained); err != nil {
		return datamodel.Field{}, err
	}
	return retained.field, nil
}

var _ ports.PhysicalFieldPublicationManager = (*PhysicalPublicationManager)(nil)
