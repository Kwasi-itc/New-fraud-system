package ports

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrJobQueueUnavailable = errors.New("transactional index job queue unavailable")

type MutationStore interface {
	Tenants() TenantRepository
	Tables() TableRepository
	Fields() FieldRepository
	FieldEnumValues() FieldEnumValueRepository
	Links() LinkRepository
	Pivots() PivotRepository
	TableOptions() TableOptionsRepository
	NavigationOptions() NavigationOptionRepository
	SchemaChanges() SchemaChangeRepository
	TenantSchemaMigrations() TenantSchemaMigrationRepository
	IndexJobs() IndexJobRepository
	SchemaManager() SchemaManager
	EnqueueIndexJob(context.Context, uuid.UUID, *time.Time) error
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(MutationStore) error) error
}

// TenantRegistrationManager binds a newly owner-allocated tenant identity before
// exposing repositories. It must authorize platform registration for that scope;
// this interface is not a general way to switch an existing tenant transaction.
type TenantRegistrationManager interface {
	RunRegistration(context.Context, uuid.UUID, func(MutationStore) error) error
}
