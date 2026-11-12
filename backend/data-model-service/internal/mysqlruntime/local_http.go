package mysqlruntime

import (
	"database/sql"
	"errors"
	"net/http"
	"os"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/gin-gonic/gin"
)

// Shared limits preserve the existing bounded local metadata contract.
const localMaxRows, localMaxBytes = 1000, 4 << 20

// NewLocalHTTP composes the explicitly enabled local administrator API.
// Production entrypoints do not use this router.
func NewLocalHTTP(db *sql.DB, enabled, origin string) (http.Handler, error) {
	if _, err := localdev.Origin(enabled, origin); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("MySQL pool required")
	}
	router, err := newLocalRouter(db)
	if err != nil {
		return nil, err
	}
	credential := "local-data-model-development"
	if ec2test.Enabled() {
		value, err := os.ReadFile("/run/secrets/data_model_service_token")
		if err != nil || len(value) < 32 {
			return nil, errors.New("data-model service credential required")
		}
		credential = string(value)
	}
	return localdev.WithoutLogin(router, enabled, origin, credential)
}

func newLocalRouter(db *sql.DB) (*gin.Engine, error) {
	tenants, err := newLocalTenantHandler(db)
	if err != nil {
		return nil, err
	}
	router := gin.New()
	router.Use(gin.Recovery(), localRequestMiddleware())
	if err := registerLocalDocs(router); err != nil {
		return nil, err
	}
	registerLocalHealth(router, db)
	tenants.Register(router)
	router.GET("/v1/tenants/:tenantId/data-model", localModelHandler(db))
	registerLocalReads(router, db)
	author := newLocalAuthorClient()
	router.POST("/v1/tenants/:tenantId/tables", author.Forward)
	router.PATCH("/v1/tables/:tableId", author.Forward)
	router.POST("/v1/tables/:tableId/fields", author.Forward)
	router.PATCH("/v1/fields/:fieldId", author.Forward)
	router.DELETE("/v1/fields/:fieldId", author.Forward)
	router.PATCH("/v1/enum-values/:enumValueId", author.Forward)
	router.DELETE("/v1/enum-values/:enumValueId", author.Forward)
	router.GET("/v1/tables/:tableId/display-settings", author.Forward)
	router.PUT("/v1/tables/:tableId/display-settings", author.Forward)
	router.POST("/v1/tenants/:tenantId/record-displays", author.Forward)
	router.GET("/v1/tenants/:tenantId/data-model/export", author.Forward)
	router.POST("/v1/tenants/:tenantId/data-model/import", author.Forward)
	router.DELETE("/v1/tables/:tableId", author.Forward)
	router.POST("/v1/fields/:fieldId/enum-values", author.Forward)
	router.POST("/v1/tenants/:tenantId/links", author.Forward)
	router.DELETE("/v1/links/:linkId", author.Forward)
	router.POST("/v1/tenants/:tenantId/pivots", author.Forward)
	router.DELETE("/v1/pivots/:pivotId", author.Forward)
	router.PUT("/v1/tables/:tableId/options", author.Forward)
	router.POST("/v1/tables/:tableId/navigation-options", author.Forward)
	router.DELETE("/v1/navigation-options/:navigationOptionId", author.Forward)
	router.POST("/v1/tenants/:tenantId/index-jobs", author.Forward)
	router.GET("/v1/tenants/:tenantId/index-jobs/scoped", author.Forward)
	router.POST("/v1/index-jobs/:jobId/retry", author.Forward)
	router.GET("/v1/admin/reconcile", author.Forward)
	return router, nil
}
