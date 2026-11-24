package httpapi

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/reconcile"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
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
	PolicyObserver func([]platformauth.RoutePolicy)
	Verifier       platformauth.TokenVerifier
	UserVerifier   platformVerifier

	AllowedOrigins         []string
	IndexWorkerMaxAttempts int
	IndexJobQueueName      string
}

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	engine := gin.New()
	router := platformauth.NewRouter(engine, cfg.Verifier)
	router.Use(requestContextMiddleware(logger))
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", platformauth.Policy{Public: true}, healthHandler.Healthz)
	router.GET("/readyz", platformauth.Policy{Public: true}, healthHandler.Readyz)

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
	riverClient, _ := river.NewClient(riverpgxv5.New(db), &river.Config{})
	indexJobEnqueuer := riverjobs.NewRiverIndexJobEnqueuer(riverClient, cfg.IndexWorkerMaxAttempts, cfg.IndexJobQueueName)
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
	).WithIndexJobEnqueuer(indexJobEnqueuer)
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
	).WithIndexJobEnqueuer(indexJobEnqueuer)
	readService := service.NewDataModelReadService(
		readRepository,
		tenantRepository,
		tenantSchemaMigrationRepository,
	)
	portableService := service.NewPortableDataModelService(
		readService,
		tableService,
		fieldService,
		linkService,
		pivotService,
		optionsService,
		navigationOptionService,
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
		indexJobEnqueuer,
	)
	dataModelHandler := handlers.NewDataModelHandler(
		readService,
		portableService,
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
	reconcileHandler := handlers.NewReconcileHandler(reconcile.NewService(db, indexJobEnqueuer))

	v1 := router.Group("/v1")
	v1.Use(bindPlatformGrant(db))
	v1.POST("/tenants", platformauth.Policy{Capability: "tenants:provision", PrincipalType: platformauth.PrincipalService, ControlPlane: true}, tenantHandler.Create)
	v1.GET("/tenants", platformauth.Policy{Capability: "tenants:provision", PrincipalType: platformauth.PrincipalService, ControlPlane: true}, tenantHandler.List)
	v1.GET("/tenants/:tenantId", platformauth.Policy{Capability: "models:read"}, tenantHandler.Get)
	v1.POST("/tenants/:tenantId/provision", platformauth.Policy{Capability: "tenants:provision", PrincipalType: platformauth.PrincipalService}, tenantHandler.Provision)
	v1.GET("/tenants/:tenantId/data-model", platformauth.Policy{Capability: "models:read"}, dataModelHandler.GetDataModel)
	v1.GET("/tenants/:tenantId/data-model/export", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ExportDataModel)
	v1.POST("/tenants/:tenantId/data-model/import", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.ImportDataModel)
	v1.GET("/tenants/:tenantId/tables", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ListTables)
	v1.POST("/tenants/:tenantId/tables", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.CreateTable)
	v1.GET("/tables/:tableId/fields", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ListFields)
	v1.POST("/tables/:tableId/fields", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.CreateField)
	v1.GET("/fields/:fieldId/enum-values", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ListFieldEnumValues)
	v1.POST("/fields/:fieldId/enum-values", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.CreateFieldEnumValue)
	v1.GET("/tenants/:tenantId/links", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ListLinks)
	v1.POST("/tenants/:tenantId/links", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.CreateLink)
	v1.GET("/tenants/:tenantId/pivots", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ListPivots)
	v1.POST("/tenants/:tenantId/pivots", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.CreatePivot)
	v1.GET("/tables/:tableId/options", platformauth.Policy{Capability: "models:read"}, dataModelHandler.GetOptions)
	displayHandler := handlers.DisplayHandler{Store: display.Store{DB: db, Transactions: transactionManager, SetCaption: tableService.UpdateDisplayCaption}}
	v1.GET("/tables/:tableId/display-settings", platformauth.Policy{Capability: "models:read"}, displayHandler.Get)
	v1.PUT("/tables/:tableId/display-settings", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, displayHandler.Save)
	v1.POST("/tenants/:tenantId/record-displays", platformauth.Policy{Capability: "models:read", PrincipalType: platformauth.PrincipalUser}, displayHandler.Read)
	v1.PUT("/tables/:tableId/options", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.UpsertOptions)
	v1.GET("/tables/:tableId/navigation-options", platformauth.Policy{Capability: "models:read"}, dataModelHandler.ListNavigationOptions)
	v1.POST("/tables/:tableId/navigation-options", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.CreateNavigationOption)
	v1.GET("/tenants/:tenantId/schema-change-log", platformauth.Policy{Capability: "models:read"}, schemaChangeHandler.List)
	v1.GET("/tenants/:tenantId/schema-migrations", platformauth.Policy{Capability: "models:read"}, tenantSchemaMigrationHandler.List)
	v1.POST("/tenants/:tenantId/index-jobs", platformauth.Policy{Capability: "models:index-write"}, indexJobHandler.Create)
	v1.GET("/tenants/:tenantId/index-jobs", platformauth.Policy{Capability: "models:read"}, indexJobHandler.List)
	v1.GET("/index-jobs/:jobId", platformauth.Policy{Capability: "models:read"}, indexJobHandler.Get)
	v1.POST("/index-jobs/:jobId/retry", platformauth.Policy{Capability: "models:index-write"}, indexJobHandler.Retry)
	v1.PATCH("/tables/:tableId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.UpdateTable)
	v1.PATCH("/fields/:fieldId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.UpdateField)
	v1.PATCH("/enum-values/:enumValueId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.UpdateFieldEnumValue)
	v1.DELETE("/tables/:tableId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.DeleteTable)
	v1.DELETE("/fields/:fieldId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.DeleteField)
	v1.DELETE("/enum-values/:enumValueId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.DeleteFieldEnumValue)
	v1.DELETE("/links/:linkId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.DeleteLink)
	v1.DELETE("/pivots/:pivotId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.DeletePivot)
	v1.DELETE("/navigation-options/:navigationOptionId", platformauth.Policy{Capability: "models:write", PrincipalType: platformauth.PrincipalUser}, dataModelHandler.DeleteNavigationOption)
	v1.GET("/admin/reconcile", platformauth.Policy{Capability: "platform:reconcile", PrincipalType: platformauth.PrincipalService, ControlPlane: true}, reconcileHandler.Run)

	if err := router.ValidateCoverage(); err != nil {
		panic(err)
	}
	if cfg.PolicyObserver != nil {
		cfg.PolicyObserver(router.Inventory())
	}
	return engine
}
