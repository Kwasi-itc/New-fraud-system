package service

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

// presentationResultPublisher reuses the complete existing owner validation and
// effects. The result manager owns commit/replay; the provisional publication is
// intentionally discarded by the typed response entry point below.
type presentationResultPublisher func(context.Context, ports.MetadataPublicationRequest, func(ports.MutationStore) error) (contract.Publication, error)

func (p presentationResultPublisher) Publish(ctx context.Context, r ports.MetadataPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
	return p(ctx, r, effects)
}

func (s TableService) PublishPresentationResult(ctx context.Context, input UpdateTableInput, r ports.MetadataPublicationRequest, manager ports.MetadataResultPublicationManager) (datamodel.Table, error) {
	if manager == nil {
		return datamodel.Table{}, errors.New("original table result publisher required")
	}
	var result datamodel.Table
	_, err := s.PublishPresentationUpdate(ctx, input, r, presentationResultPublisher(func(ctx context.Context, r ports.MetadataPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
		var err error
		result, err = manager.PublishTable(ctx, r, effects)
		return contract.Publication{}, err
	}))
	if err != nil {
		return datamodel.Table{}, err
	}
	return result, nil
}
func (s FieldService) PublishPresentationResult(ctx context.Context, input UpdateFieldInput, r ports.MetadataPublicationRequest, manager ports.MetadataResultPublicationManager) (datamodel.Field, error) {
	if manager == nil {
		return datamodel.Field{}, errors.New("original field result publisher required")
	}
	var result datamodel.Field
	_, err := s.PublishPresentationUpdate(ctx, input, r, presentationResultPublisher(func(ctx context.Context, r ports.MetadataPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
		var err error
		result, err = manager.PublishField(ctx, r, input.FieldID, effects)
		return contract.Publication{}, err
	}))
	if err != nil {
		return datamodel.Field{}, err
	}
	return result, nil
}
