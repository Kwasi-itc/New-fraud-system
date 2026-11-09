package mysqlruntime

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerLocalRelationshipReads(router *gin.Engine, db *sql.DB) {
	mountLocalRead(router, db, "/v1/tenants/:tenantId/links", "tenantId", "", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Links.ListByTenant(ctx, id)
		return adaptLocalList(v, e, dto.AdaptLink, "links")
	})
	mountLocalRead(router, db, "/v1/tenants/:tenantId/pivots", "tenantId", "", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Pivots.ListByTenant(ctx, id)
		return adaptLocalList(v, e, dto.AdaptPivot, "pivots")
	})
	mountLocalRead(router, db, "/v1/tables/:tableId/navigation-options", "tableId", "data_model_tables", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Navigation.ListBySourceTable(ctx, id)
		return adaptLocalList(v, e, dto.AdaptNavigationOption, "navigation_options")
	})
}
