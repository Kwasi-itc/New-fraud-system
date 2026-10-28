package ports

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/google/uuid"
)

type MetadataPublicationRequest struct {
	TenantID, TableID, ActorID, OperationID uuid.UUID
	RequestHash                             [32]byte
	Condition                               contract.PublicationCondition
}

// MetadataPublicationManager permits single-table presentation-only effects on
// an existing published model. It must authorize every call/replay, keep the
// matching open physical gate locked through commit, reject changed ingestion
// semantics, and atomically persist owner effects and immutable receipt. Replay
// returns the original publication, not an original domain-resource result.
type MetadataPublicationManager interface {
	Publish(context.Context, MetadataPublicationRequest, func(MutationStore) error) (contract.Publication, error)
}

// MetadataResultPublicationManager retains typed original owner responses with
// the publication. Every replay requires current authorization. Missing legacy
// results fail closed, and no response is returned before owner commit succeeds.
type MetadataResultPublicationManager interface {
	PublishTable(context.Context, MetadataPublicationRequest, func(MutationStore) error) (datamodel.Table, error)
	PublishField(context.Context, MetadataPublicationRequest, uuid.UUID, func(MutationStore) error) (datamodel.Field, error)
}
