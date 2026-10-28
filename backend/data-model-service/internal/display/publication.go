package display

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// PublicationRequest carries trusted identity and the original command. The
// manager must authorize every invocation, including receipt replay. Results are
// ingestion publications, not snapshots of saved or current display settings.
type PublicationRequest struct {
	TenantID, TableID, ActorID, OperationID uuid.UUID
	Condition                               contract.PublicationCondition
	Settings                                SaveRequest
}

// PublicationManager coordinates ordered display dependency locks, validation,
// open physical-gate checks and atomic settings/owner effects/audit/receipt.
// Effects use only local transaction-bound repositories; no DDL/network calls,
// nested transactions or retained stores are allowed. Replay skips effects.
type PublicationManager interface {
	Publish(context.Context, PublicationRequest, func(ports.MutationStore) error) (contract.Publication, error)
}

// SettingsPublicationManager retains the original settings response atomically
// with publication. Every replay requires current authorization and must not
// substitute current settings when the original result is unavailable.
type SettingsPublicationManager interface {
	PublishSettings(context.Context, PublicationRequest, func(ports.MutationStore) error) (Settings, error)
}
