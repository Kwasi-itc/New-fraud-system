package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

// The same mandatory local-SQL service-identity boundary is used for all kinds.
// Authorization must check the exact tenant, kind and action, not merely accept
// possession of a tenant UUID. No provider is installed by this staged adapter.
type ExecutionWorkerAuthorizer = EffectWorkerAuthorizer
type ExecutionWorkerAction = EffectWorkerAction

const (
	ExecutionWorkerClaim     = EffectWorkerClaim
	ExecutionWorkerHeartbeat = EffectWorkerHeartbeat
	ExecutionWorkerRescue    = EffectWorkerRescue
	ExecutionWorkerCleanup   = EffectWorkerCleanup
)

// ExecutionDeliveryStore exposes ONLY supported transport primitives. Owner
// completion/retry/callback/redrive goes through ExecutionStore's atomic owner
// lifecycle, not the four-effect generic Finish callback. Claim does not run a
// business handler or activate a poller. Rescue/exhaustion remain separate work.
type ExecutionDeliveryStore struct{ transport *EffectDeliveryStore }

func NewExecutionDeliveryStore(db *sql.DB, database string, tenant uuid.UUID, kind ports.JobKind, queue string, lease time.Duration, authorize ExecutionWorkerAuthorizer) (*ExecutionDeliveryStore, error) {
	s, err := newScopedDeliveryStore(db, database, tenant, kind, queue, lease, authorize, true)
	if err != nil {
		return nil, err
	}
	return &ExecutionDeliveryStore{transport: s}, nil
}

func (s *ExecutionDeliveryStore) Claim(ctx context.Context) (*ExecutionLease, error) {
	if s == nil || s.transport == nil {
		return nil, errors.New("initialized execution transport required")
	}
	v, err := s.transport.Claim(ctx)
	if err != nil || v == nil {
		return nil, err
	}
	return &ExecutionLease{ID: v.ID, Token: v.Token, Identity: v.Identity, TransportAttempt: v.Attempt}, nil
}
func (s *ExecutionDeliveryStore) Heartbeat(ctx context.Context, v ExecutionLease) error {
	if s == nil || s.transport == nil {
		return errors.New("initialized execution transport required")
	}
	err := s.transport.Heartbeat(ctx, EffectDeliveryLease{ID: v.ID, Token: v.Token, Identity: v.Identity, Attempt: v.TransportAttempt})
	if errors.Is(err, ErrStaleEffectLease) {
		return ErrExecutionLease
	}
	return err
}
