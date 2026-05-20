package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"sort"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type DataModelReadService struct {
	repository       ports.DataModelReadRepository
	tenantRepository ports.TenantRepository
	migrationRepo    ports.TenantSchemaMigrationRepository
}

type PublishedAssembledDataModel struct {
	RevisionID string
	Tenant     tenant.Tenant
	Model      datamodel.AssembledDataModel
}

func NewDataModelReadService(
	repository ports.DataModelReadRepository,
	tenantRepository ports.TenantRepository,
	migrationRepo ports.TenantSchemaMigrationRepository,
) DataModelReadService {
	return DataModelReadService{
		repository:       repository,
		tenantRepository: tenantRepository,
		migrationRepo:    migrationRepo,
	}
}

func (s DataModelReadService) Get(ctx context.Context, tenantID uuid.UUID) (PublishedAssembledDataModel, error) {
	record, err := s.tenantRepository.GetByID(ctx, tenantID)
	if err != nil {
		return PublishedAssembledDataModel{}, err
	}

	model, err := s.repository.GetAssembledDataModel(ctx, tenantID)
	if err != nil {
		return PublishedAssembledDataModel{}, err
	}

	migrations, err := s.migrationRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return PublishedAssembledDataModel{}, err
	}

	return PublishedAssembledDataModel{
		RevisionID: buildRevisionID(record, migrations),
		Tenant:     record,
		Model:      model,
	}, nil
}

func buildRevisionID(record tenant.Tenant, migrations []datamodel.TenantSchemaMigration) string {
	sort.Slice(migrations, func(i, j int) bool {
		if migrations[i].AppliedAt.Equal(migrations[j].AppliedAt) {
			if migrations[i].Version == migrations[j].Version {
				return migrations[i].ID.String() < migrations[j].ID.String()
			}
			return migrations[i].Version < migrations[j].Version
		}
		return migrations[i].AppliedAt.Before(migrations[j].AppliedAt)
	})

	h := sha1.New()
	h.Write([]byte(record.ID.String()))
	h.Write([]byte("|"))
	h.Write([]byte(string(record.Status)))
	h.Write([]byte("|"))
	for _, migration := range migrations {
		h.Write([]byte(migration.ID.String()))
		h.Write([]byte("|"))
		h.Write([]byte(migration.Version))
		h.Write([]byte("|"))
		h.Write([]byte(migration.AppliedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")))
		h.Write([]byte("|"))
	}

	return hex.EncodeToString(h.Sum(nil))
}
