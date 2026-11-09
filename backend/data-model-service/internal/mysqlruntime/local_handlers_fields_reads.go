package mysqlruntime

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerLocalFieldReads(router *gin.Engine, db *sql.DB) {
	mountLocalRead(router, db, "/v1/tables/:tableId/fields", "tableId", "data_model_tables", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Fields.ListByTable(ctx, id)
		return adaptLocalList(v, e, dto.AdaptField, "fields")
	})
	mountLocalRead(router, db, "/v1/fields/:fieldId/enum-values", "fieldId", "data_model_fields", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Enums.ListByField(ctx, id)
		return adaptLocalList(v, e, dto.AdaptFieldEnumValue, "enum_values")
	})
}
