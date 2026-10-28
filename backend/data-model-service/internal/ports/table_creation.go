package ports

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

// TableCreationSnapshot is immutable original owner output, retained before DDL.
// Returning it from preparation does not mean the physical table is ready.
type TableCreationSnapshot struct {
	Format string             `json:"format"`
	Table  datamodel.Table    `json:"table"`
	Fields [2]datamodel.Field `json:"fields"`
}

type TableCreationRequest struct {
	TenantID, OperationID, ActorID uuid.UUID
	RequestHash                    [32]byte
}

type TableCreationAllocation struct {
	Snapshot        TableCreationSnapshot
	PublicationHash [32]byte
}

// Allocate retains owner-generated identities and metadata with a closed gate.
// build is local/pure except for ID/time allocation and is skipped on replay.
// Recover returns the original Table only after verified publication commits.
type TableCreationManager interface {
	Allocate(context.Context, TableCreationRequest, func() (TableCreationAllocation, error)) (TableCreationAllocation, error)
	Recover(context.Context, TableCreationRequest) (datamodel.Table, error)
}
