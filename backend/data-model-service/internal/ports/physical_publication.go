package ports

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/google/uuid"
)

// PhysicalPublicationRequest is an owner-authorized command identity matching a
// durable DDL intent. RequestHash binds the canonical mutation, scope, actor and
// version condition. Actor values must come from trusted identity, not headers.
type PhysicalPublicationRequest struct {
	TableID            uuid.UUID
	OperationID        uuid.UUID
	RequestHash        [32]byte
	Condition          contract.PublicationCondition
	ActorKind, ActorID string
}

// PhysicalPublicationManager publishes one already-executed durable physical
// operation. Implementations must reauthorize every invocation, including replay,
// verify shape/grants under the execution lock, and atomically commit owner
// effects, authoritative contract, receipt and readiness. A historical replay
// returns the original result without running mutate or reopening a newer gate.
//
// mutate uses only local repositories/queue, performs business validation and
// audit/revision effects, and must not issue physical DDL, call external services
// or retain the store. Every error aborts owner effects; an ambiguous commit must
// be reconciled with the same operation identity, never automatically retried.
// This single-table port does not authorize multi-table display/retirement work.
type PhysicalPublicationManager interface {
	Publish(context.Context, PhysicalPublicationRequest, func(MutationStore) error) (contract.Publication, error)
}

// PhysicalFieldPublicationManager retains the original Field response atomically
// with owner effects, publication and readiness under the execution lock. Replay
// reauthorizes and returns the retained response, never current resource state.
type PhysicalFieldPublicationManager interface {
	PublishField(context.Context, PhysicalPublicationRequest, uuid.UUID, func(MutationStore) error) (datamodel.Field, error)
}
