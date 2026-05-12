package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/tenant"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/ports"
)

type TenantService struct {
	repository    ports.TenantRepository
	schemaChanges ports.SchemaChangeRepository
	schemaManager ports.SchemaManager
	txManager     ports.TransactionManager
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
		schemaChanges: schemaChanges,
		schemaManager: schemaManager,
		txManager:     txManager,
		idGenerator:   idGenerator,
		clock:         clock,
	}
}

func (s TenantService) Create(ctx context.Context, input tenant.CreateInput) (tenant.Tenant, error) {
	if err := tenant.ValidateCreate(input); err != nil {
		return tenant.Tenant{}, err
	}

	record := tenant.New(s.idGenerator.New(), s.clock.Now(), input.Name, input.ExternalKey)
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Tenants().Create(ctx, record); err != nil {
			return fmt.Errorf("create tenant: %w", err)
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
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
		))
		return nil
	}); err != nil {
		return tenant.Tenant{}, err
	}

	return record, nil
}

func (s TenantService) Get(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	return s.repository.GetByID(ctx, id)
}

func (s TenantService) List(ctx context.Context) ([]tenant.Tenant, error) {
	return s.repository.List(ctx)
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
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			record.ID,
			"provision_tenant_schema",
			"tenant",
			record.ID,
			now,
			map[string]any{
				"schema_name": record.SchemaName,
			},
		))

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
