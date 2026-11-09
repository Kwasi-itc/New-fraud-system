package mysqlruntime

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerLocalTableReads(router *gin.Engine, db *sql.DB) {
	mountLocalRead(router, db, "/v1/tenants/:tenantId/tables", "tenantId", "", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Tables.ListAvailableByTenant(ctx, id)
		return adaptLocalList(v, e, dto.AdaptTable, "tables")
	})
	mountLocalRead(router, db, "/v1/tables/:tableId/options", "tableId", "data_model_tables", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		options, err := r.Options.GetByTableID(ctx, id)
		if err != nil {
			return nil, err
		}
		if options == nil {
			table, err := r.Tables.GetByID(ctx, id)
			if err != nil {
				return nil, err
			}
			options = &datamodel.TableOptions{TableID: id, TenantID: table.TenantID}
		}
		fields, err := r.Fields.ListByTable(ctx, id)
		if err != nil {
			return nil, err
		}
		return dto.AdaptTableOptions(*options, fields), nil
	})
}
