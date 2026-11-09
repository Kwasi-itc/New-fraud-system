package localdelivery

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localtenants"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

// MultiProvider uses one shared pool, one bounded background loop, and fresh
// tenant-scoped owners. No tenant graph or mutable authority is cached.
type MultiProvider struct {
	db              *sql.DB
	database, token string
	registry        *localtenants.Registry
	reject          bool
}

func NewMultiProvider(db *sql.DB, database, token string) (*MultiProvider, error) {
	if len(token) != 64 {
		return nil, ErrRequest
	}
	registry, err := localtenants.New(db, database)
	if err != nil {
		return nil, err
	}
	return &MultiProvider{db: db, database: database, token: token, registry: registry}, nil
}
func (h *MultiProvider) SetCallbackPolicy(reject bool) { h.reject = reject }
func (h *MultiProvider) scoped(tenant uuid.UUID) (*Provider, error) {
	if tenant == uuid.Nil {
		return nil, ErrRequest
	}
	auth := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ bool) error {
		if id != tenant {
			return ErrRequest
		}
		return h.registry.Authorize(ctx, tx, id)
	}
	p, err := NewProvider(h.db, h.database, h.token, tenant, auth)
	if err == nil {
		p.SetCallbackPolicy(h.reject)
	}
	return p, err
}
func (h *MultiProvider) Handler() http.Handler {
	return providerHandler(h.token, h.db, func(r ports.DispatchRequest) (*Provider, error) {
		tenant, err := uuid.Parse(r.TenantID)
		if err != nil || Validate(r, tenant) != nil {
			return nil, ErrRequest
		}
		return h.scoped(tenant)
	})
}
func (h *MultiProvider) Run(ctx context.Context) error {
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	var after uuid.UUID
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timer.C:
			tick, cancel := context.WithTimeout(ctx, 5*time.Second)
			tenant, next, err := h.registry.Next(tick, after)
			if err == nil {
				after = next
				if tenant != uuid.Nil {
					var p *Provider
					p, err = h.scoped(tenant)
					if err == nil {
						err = p.ProcessOnce(tick)
					}
				}
			}
			cancel()
			if err != nil && !errors.Is(err, localtenants.ErrDenied) && !errors.Is(err, context.Canceled) {
				slog.Error("local multi-tenant provider processing failed", "tenant_id", tenant, "error", err)
			}
		}
	}
}
