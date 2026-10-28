package service

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type physicalResultPublisher func(context.Context, ports.PhysicalPublicationRequest, func(ports.MutationStore) error) (contract.Publication, error)

func (p physicalResultPublisher) Publish(ctx context.Context, r ports.PhysicalPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
	return p(ctx, r, effects)
}

// retainPhysicalField reuses each owner's complete validation/effects. The typed
// manager controls commit and replay; no provisional resource escapes on error.
func retainPhysicalField(id uuid.UUID, manager ports.PhysicalFieldPublicationManager, owner func(ports.PhysicalPublicationManager) error) (datamodel.Field, error) {
	if manager == nil {
		return datamodel.Field{}, errors.New("original physical Field publisher required")
	}
	var result datamodel.Field
	err := owner(physicalResultPublisher(func(ctx context.Context, r ports.PhysicalPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
		var err error
		result, err = manager.PublishField(ctx, r, id, effects)
		return contract.Publication{}, err
	}))
	if err != nil {
		return datamodel.Field{}, err
	}
	return result, nil
}

func (s FieldService) PublishPreparedCreationResult(ctx context.Context, tenant, id uuid.UUID, input CreateFieldInput, r ports.PhysicalPublicationRequest, manager ports.PhysicalFieldPublicationManager) (datamodel.Field, error) {
	return retainPhysicalField(id, manager, func(m ports.PhysicalPublicationManager) error {
		_, err := s.PublishPreparedCreation(ctx, tenant, id, input, r, m)
		return err
	})
}
func (s FieldService) PublishPreparedNullabilityResult(ctx context.Context, tenant uuid.UUID, input UpdateFieldInput, r ports.PhysicalPublicationRequest, manager ports.PhysicalFieldPublicationManager) (datamodel.Field, error) {
	return retainPhysicalField(input.FieldID, manager, func(m ports.PhysicalPublicationManager) error {
		_, err := s.PublishPreparedNullabilityUpdate(ctx, tenant, input, r, m)
		return err
	})
}
func (s FieldService) PublishPreparedUniqueResult(ctx context.Context, tenant uuid.UUID, input UpdateFieldInput, r ports.PhysicalPublicationRequest, manager ports.PhysicalFieldPublicationManager) (datamodel.Field, error) {
	return retainPhysicalField(input.FieldID, manager, func(m ports.PhysicalPublicationManager) error {
		_, err := s.PublishPreparedUniqueEnable(ctx, tenant, input, r, m)
		return err
	})
}
