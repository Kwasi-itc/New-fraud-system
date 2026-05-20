package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/reconcile"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
)

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type RouterConfig struct {
	AuthMode  string
	AuthToken string
}

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	router := gin.New()
	router.Use(requestContextMiddleware(logger))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)

	tenantRepository := storepostgres.NewTenantRepository(db)
	tableRepository := storepostgres.NewTableRepository(db)
	fieldRepository := storepostgres.NewFieldRepository(db)
	fieldEnumValueRepository := storepostgres.NewFieldEnumValueRepository(db)
	linkRepository := storepostgres.NewLinkRepository(db)
	pivotRepository := storepostgres.NewPivotRepository(db)
	optionsRepository := storepostgres.NewTableOptionsRepository(db)
	navigationOptionRepository := storepostgres.NewNavigationOptionRepository(db)
	readRepository := storepostgres.NewDataModelReadRepository(db)
	schemaChangeRepository := storepostgres.NewSchemaChangeRepository(db)
	tenantSchemaMigrationRepository := storepostgres.NewTenantSchemaMigrationRepository(db)
	indexJobRepository := storepostgres.NewIndexJobRepository(db)
	schemaManager := tenantdbpostgres.NewSchemaManager(db)
	transactionManager := storepostgres.NewTransactionManager(db)
	tenantService := service.NewTenantService(
		tenantRepository,
		schemaChangeRepository,
		schemaManager,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	tenantHandler := handlers.NewTenantHandler(tenantService)
	tableService := service.NewTableService(
		tenantRepository,
		tableRepository,
		fieldRepository,
		linkRepository,
		pivotRepository,
		schemaChangeRepository,
		schemaManager,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	fieldService := service.NewFieldService(
		tenantRepository,
		tableRepository,
		fieldRepository,
		fieldEnumValueRepository,
		linkRepository,
		pivotRepository,
		schemaChangeRepository,
		schemaManager,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	linkService := service.NewLinkService(
		tableRepository,
		fieldRepository,
		linkRepository,
		pivotRepository,
		schemaChangeRepository,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	enumValueService := service.NewFieldEnumValueService(
		fieldRepository,
		fieldEnumValueRepository,
		schemaChangeRepository,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	pivotService := service.NewPivotService(
		tableRepository,
		fieldRepository,
		linkRepository,
		pivotRepository,
		schemaChangeRepository,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	optionsService := service.NewOptionsService(
		tableRepository,
		fieldRepository,
		optionsRepository,
		schemaChangeRepository,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	navigationOptionService := service.NewNavigationOptionService(
		tableRepository,
		fieldRepository,
		linkRepository,
		pivotRepository,
		navigationOptionRepository,
		schemaChangeRepository,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	readService := service.NewDataModelReadService(
		readRepository,
		tenantRepository,
		tenantSchemaMigrationRepository,
	)
	indexJobService := service.NewIndexJobService(
		tenantRepository,
		tableRepository,
		fieldRepository,
		indexJobRepository,
		schemaChangeRepository,
		transactionManager,
		uuidGenerator{},
		systemClock{},
	)
	dataModelHandler := handlers.NewDataModelHandler(
		readService,
		tableService,
		fieldService,
		enumValueService,
		linkService,
		pivotService,
		optionsService,
		navigationOptionService,
	)
	schemaChangeHandler := handlers.NewSchemaChangeHandler(service.NewSchemaChangeService(schemaChangeRepository))
	tenantSchemaMigrationHandler := handlers.NewTenantSchemaMigrationHandler(service.NewTenantSchemaMigrationService(tenantSchemaMigrationRepository))
	indexJobHandler := handlers.NewIndexJobHandler(indexJobService)
	reconcileHandler := handlers.NewReconcileHandler(reconcile.NewService(db))

	v1 := router.Group("/v1")
	v1.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	v1.POST("/tenants", tenantHandler.Create)
	v1.GET("/tenants", tenantHandler.List)
	v1.GET("/tenants/:tenantId", tenantHandler.Get)
	v1.POST("/tenants/:tenantId/provision", tenantHandler.Provision)
	v1.GET("/tenants/:tenantId/data-model", dataModelHandler.GetDataModel)
	v1.GET("/tenants/:tenantId/tables", dataModelHandler.ListTables)
	v1.POST("/tenants/:tenantId/tables", dataModelHandler.CreateTable)
	v1.GET("/tables/:tableId/fields", dataModelHandler.ListFields)
	v1.POST("/tables/:tableId/fields", dataModelHandler.CreateField)
	v1.GET("/fields/:fieldId/enum-values", dataModelHandler.ListFieldEnumValues)
	v1.POST("/fields/:fieldId/enum-values", dataModelHandler.CreateFieldEnumValue)
	v1.GET("/tenants/:tenantId/links", dataModelHandler.ListLinks)
	v1.POST("/tenants/:tenantId/links", dataModelHandler.CreateLink)
	v1.GET("/tenants/:tenantId/pivots", dataModelHandler.ListPivots)
	v1.POST("/tenants/:tenantId/pivots", dataModelHandler.CreatePivot)
	v1.GET("/tables/:tableId/options", dataModelHandler.GetOptions)
	v1.PUT("/tables/:tableId/options", dataModelHandler.UpsertOptions)
	v1.GET("/tables/:tableId/navigation-options", dataModelHandler.ListNavigationOptions)
	v1.POST("/tables/:tableId/navigation-options", dataModelHandler.CreateNavigationOption)
	v1.GET("/tenants/:tenantId/schema-change-log", schemaChangeHandler.List)
	v1.GET("/tenants/:tenantId/schema-migrations", tenantSchemaMigrationHandler.List)
	v1.POST("/tenants/:tenantId/index-jobs", indexJobHandler.Create)
	v1.GET("/tenants/:tenantId/index-jobs", indexJobHandler.List)
	v1.GET("/index-jobs/:jobId", indexJobHandler.Get)
	v1.POST("/index-jobs/:jobId/retry", indexJobHandler.Retry)
	v1.PATCH("/tables/:tableId", dataModelHandler.UpdateTable)
	v1.PATCH("/fields/:fieldId", dataModelHandler.UpdateField)
	v1.PATCH("/enum-values/:enumValueId", dataModelHandler.UpdateFieldEnumValue)
	v1.DELETE("/tables/:tableId", dataModelHandler.DeleteTable)
	v1.DELETE("/fields/:fieldId", dataModelHandler.DeleteField)
	v1.DELETE("/enum-values/:enumValueId", dataModelHandler.DeleteFieldEnumValue)
	v1.DELETE("/links/:linkId", dataModelHandler.DeleteLink)
	v1.DELETE("/pivots/:pivotId", dataModelHandler.DeletePivot)
	v1.DELETE("/navigation-options/:navigationOptionId", dataModelHandler.DeleteNavigationOption)
	v1.GET("/admin/reconcile", reconcileHandler.Run)

	return router
}
