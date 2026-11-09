package mysql

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// NewPublishedDataModelReadRepository opts into the physical publication contract.
// The caller must authorize the tenant and grant SELECT on the owner registry.
// Metadata-only callers keep using NewDataModelReadRepository. Neither reader
// takes locks across HTTP; consumers must recheck the version in their data tx.
func NewPublishedDataModelReadRepository(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int) (*DataModelReadRepository, error) {
	r, err := NewDataModelReadRepository(db, database, tenant, maxRows, maxBytes)
	if err != nil {
		return nil, err
	}
	r.published = true
	return r, nil
}

func (r *DataModelReadRepository) loadModel(ctx context.Context, budget metadataRepository) (datamodel.AssembledDataModel, error) {
	model, err := loadAssembledModelVisibility(ctx, &budget, r.published)
	if err != nil || !r.published {
		return model, err
	}
	// One bounded tenant-indexed projection, in the same snapshot as metadata
	// and revision. A closed or active gate deliberately supplies no usable token.
	type publication struct{ table, version uuid.UUID }
	versions, err := aggregateList(ctx, &budget, "SELECT table_id,published_version FROM "+r.schema+".data_model_physical_tables WHERE tenant_id=? AND write_state='open' AND active_operation IS NULL AND published_version IS NOT NULL ORDER BY table_id", func(row metadataScanner) (publication, int64, error) {
		var p publication
		err := row.Scan((*mysqlstore.UUID)(&p.table), (*mysqlstore.UUID)(&p.version))
		return p, 0, err
	})
	if err != nil {
		return datamodel.AssembledDataModel{}, err
	}
	byID := make(map[uuid.UUID]uuid.UUID, len(versions))
	for _, p := range versions {
		byID[p.table] = p.version
	}
	for name, table := range model.Tables {
		if version := byID[table.ID]; version != uuid.Nil && !table.Archived {
			table.PublishedVersion = &version
			model.Tables[name] = table
		}
	}
	return model, nil
}
