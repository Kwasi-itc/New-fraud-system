package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

var ErrTenantRegistrationExists = errors.New("tenant registration scope already exists; explicit operation recovery required")

// RegistrationTransactionManager is an explicit platform-registration capability,
// not a replacement for tenant-scoped owner transactions. Its authorizer must
// validate trusted platform registration authority for each new tenant identity.
// The immutable configuration is copied per invocation; concurrent registrations
// never share mutable tenant scope.
type RegistrationTransactionManager struct{ configuration *TransactionManager }

func NewRegistrationTransactionManager(db *sql.DB, database string, maxRows, maxBytes int, authorize MutationAuthorizer) (*RegistrationTransactionManager, error) {
	configuration, err := newTransactionConfiguration(db, database, maxRows, maxBytes, authorize, nil)
	if err != nil {
		return nil, err
	}
	return &RegistrationTransactionManager{configuration}, nil
}

func (m *RegistrationTransactionManager) RunRegistration(ctx context.Context, tenantID uuid.UUID, fn func(ports.MutationStore) error) error {
	if m == nil || m.configuration == nil || tenantID == uuid.Nil || fn == nil {
		return errors.New("initialized registration manager, nonzero tenant and callback required")
	}
	scoped := *m.configuration
	scoped.tenant = tenantID
	return scoped.Run(ctx, func(store ports.MutationStore) error {
		if _, err := store.Tenants().GetByID(ctx, tenantID); err == nil {
			return ErrTenantRegistrationExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := fn(store); err != nil {
			return err
		}
		record, err := store.Tenants().GetByID(ctx, tenantID)
		if err != nil {
			return err
		}
		if record.Status != tenant.StatusPending {
			return errors.New("registration must persist a pending tenant")
		}
		return nil
	})
}

var _ ports.TenantRegistrationManager = (*RegistrationTransactionManager)(nil)
