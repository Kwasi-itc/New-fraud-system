package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type TenantService struct {
	repository    ports.TenantRepository
	catalog       ports.TenantCatalog
	schemaChanges ports.SchemaChangeRepository
	schemaManager ports.SchemaManager
	txManager     ports.TransactionManager
	registration  ports.TenantRegistrationManager
	idGenerator   ports.IDGenerator
	clock         ports.Clock
}

func NewTenantService(
	repository ports.TenantRepository,
	schemaChanges ports.SchemaChangeRepository,
	schemaManager ports.SchemaManager,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) TenantService {
	return TenantService{
		repository:    repository,
		catalog:       repository,
		schemaChanges: schemaChanges,
		schemaManager: schemaManager,
		txManager:     txManager,
		idGenerator:   idGenerator,
		clock:         clock,
	}
}

// WithTenantCatalog replaces only listing, preserving tenant-scoped lookups and
// writes. The supplied platform reader must authorize every catalogue request.
// Existing PostgreSQL construction retains its original repository list path.
func (s TenantService) WithTenantCatalog(catalog ports.TenantCatalog) (TenantService, error) {
	if catalog == nil {
		return TenantService{}, errors.New("tenant catalogue required")
	}
	s.catalog = catalog
	return s, nil
}

func (s TenantService) Create(ctx context.Context, input tenant.CreateInput) (tenant.Tenant, error) {
	if err := tenant.ValidateCreate(input); err != nil {
		return tenant.Tenant{}, err
	}

	if s.idGenerator == nil || s.clock == nil || (s.registration == nil && s.txManager == nil) {
		return tenant.Tenant{}, errors.New("tenant registration dependencies required")
	}
	id := s.idGenerator.New()
	if id == uuid.Nil {
		return tenant.Tenant{}, errors.New("nonzero tenant identity required")
	}
	record := tenant.New(id, s.clock.Now(), input.Name, input.ExternalKey)
	mutate := func(store ports.MutationStore) error {
		if err := store.Tenants().Create(ctx, record); err != nil {
			return fmt.Errorf("create tenant: %w", err)
		}
		if err := recordSchemaChange(ctx, store.SchemaChanges(),
			s.idGenerator.New(),
			record.ID,
			"create_tenant",
			"tenant",
			record.ID,
			record.CreatedAt,
			map[string]any{
				"name":         record.Name,
				"external_key": record.ExternalKey,
				"schema_name":  record.SchemaName,
			},
		); err != nil {
			return err
		}
		if err := recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, record.ID, schemaMigrationVersion("create_tenant", "tenant"), record.CreatedAt); err != nil {
			return err
		}
		return nil
	}
	var err error
	if s.registration != nil {
		err = s.registration.RunRegistration(ctx, record.ID, mutate)
	} else {
		err = s.txManager.Run(ctx, mutate)
	}
	if err != nil {
		return tenant.Tenant{}, err
	}

	return record, nil
}

// WithTenantRegistrationManager changes only creation scope allocation. Existing
// tenant mutations and catalogue reads retain their separate capabilities.
func (s TenantService) WithTenantRegistrationManager(manager ports.TenantRegistrationManager) (TenantService, error) {
	if manager == nil {
		return TenantService{}, errors.New("tenant registration manager required")
	}
	s.registration = manager
	return s, nil
}

func (s TenantService) Get(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	return s.repository.GetByID(ctx, id)
}

func (s TenantService) List(ctx context.Context) ([]tenant.Tenant, error) {
	if s.catalog == nil {
		return nil, errors.New("tenant catalogue not configured")
	}
	return s.catalog.List(ctx)
}

func (s TenantService) Provision(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	record, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return tenant.Tenant{}, err
	}

	now := s.clock.Now()
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.SchemaManager().ProvisionTenantSchema(ctx, record); err != nil {
			return fmt.Errorf("provision tenant schema: %w", err)
		}
		if err := recordSchemaChange(ctx, store.SchemaChanges(),
			s.idGenerator.New(),
			record.ID,
			"provision_tenant_schema",
			"tenant",
			record.ID,
			now,
			map[string]any{
				"schema_name": record.SchemaName,
			},
		); err != nil {
			return err
		}
		if err := recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, record.ID, schemaMigrationVersion("provision_tenant_schema", "tenant"), now); err != nil {
			return err
		}

		if record.Status != tenant.StatusActive {
			if err := store.Tenants().UpdateStatus(ctx, id, tenant.StatusActive); err != nil {
				return fmt.Errorf("update tenant status: %w", err)
			}
		}
		return nil
	}); err != nil {
		return tenant.Tenant{}, err
	}
	if record.Status != tenant.StatusActive {
		record.Status = tenant.StatusActive
		record.UpdatedAt = now
	}

	return record, nil
}
