package mysqlruntime

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerLocalHistoryReads(router *gin.Engine, db *sql.DB) {
	mountLocalRead(router, db, "/v1/tenants/:tenantId/schema-change-log", "tenantId", "", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.History.ListByTenant(ctx, id)
		return adaptLocalList(v, e, dto.AdaptSchemaChange, "schema_changes")
	})
	mountLocalRead(router, db, "/v1/tenants/:tenantId/schema-migrations", "tenantId", "", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Revisions.ListByTenant(ctx, id)
		return adaptLocalList(v, e, dto.AdaptTenantSchemaMigration, "tenant_schema_migrations")
	})
	mountLocalRead(router, db, "/v1/tenants/:tenantId/index-jobs", "tenantId", "", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Jobs.ListByTenant(ctx, id)
		return adaptLocalList(v, e, dto.AdaptIndexJob, "index_jobs")
	})
	mountLocalRead(router, db, "/v1/index-jobs/:jobId", "jobId", "data_model_index_jobs", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
		v, e := r.Jobs.GetByID(ctx, id)
		if e != nil {
			return nil, e
		}
		return gin.H{"index_job": dto.AdaptIndexJob(v)}, nil
	})
}
