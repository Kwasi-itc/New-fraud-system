package mysql

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Presentation owners cannot use full-row UPDATE grants to alter enforcement,
// caption or lifecycle state. The publication table lock serializes these reads
// and updates; SQL only assigns the explicitly allowed presentation columns.
type presentationMutationStore struct {
	metadataMutationStore
	table uuid.UUID
}

func (s presentationMutationStore) Tables() ports.TableRepository {
	return presentationTables{s.repositories.Tables, s.table}
}
func (s presentationMutationStore) Fields() ports.FieldRepository {
	return presentationFields{s.repositories.Fields, s.table}
}

type presentationTables struct {
	TableRepository
	table uuid.UUID
}

func (r presentationTables) Update(ctx context.Context, v datamodel.Table) error {
	if v.ID != r.table || v.TenantID != r.tenant {
		return ErrPhysicalConflict
	}
	if err := r.validate(v.TenantID, v.ID, v.Description, v.Alias, v.SemanticType); err != nil {
		return err
	}
	if err := r.lock(ctx, "data_model_tables", v.ID, "", uuid.Nil); err != nil {
		return err
	}
	prior, err := r.GetByID(ctx, v.ID)
	if err != nil {
		return err
	}
	prior.Description, prior.Alias, prior.SemanticType, prior.UpdatedAt = v.Description, v.Alias, v.SemanticType, v.UpdatedAt
	if prior != v {
		return ErrPhysicalConflict
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_tables SET description=?,alias=?,semantic_type=?,updated_at=? WHERE tenant_id=? AND id=?", v.Description, v.Alias, v.SemanticType, mysqlstore.DateTime{Time: v.UpdatedAt}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ID))
	return err
}

type presentationFields struct {
	FieldRepository
	table uuid.UUID
}

func (r presentationFields) Update(ctx context.Context, v datamodel.Field) error {
	if v.TableID != r.table || v.TenantID != r.tenant {
		return ErrPhysicalConflict
	}
	if err := r.validate(v.TenantID, v.ID, v.Description, v.Alias); err != nil {
		return err
	}
	if err := r.lock(ctx, "data_model_fields", v.ID, "table_id", r.table); err != nil {
		return err
	}
	prior, err := r.GetByID(ctx, v.ID)
	if err != nil {
		return err
	}
	prior.Description, prior.Alias, prior.UpdatedAt = v.Description, v.Alias, v.UpdatedAt
	if prior != v {
		return ErrPhysicalConflict
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_fields SET description=?,alias=?,updated_at=? WHERE tenant_id=? AND table_id=? AND id=?", v.Description, v.Alias, mysqlstore.DateTime{Time: v.UpdatedAt}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(r.table), mysqlstore.UUID(v.ID))
	return err
}
