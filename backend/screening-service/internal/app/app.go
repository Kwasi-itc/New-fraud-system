package app

import (
	"context"
	"fmt"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
)

type App struct {
	cfg           Config
	logger        *slog.Logger
	db            *pgxpool.Pool
	privateWriter *pgxpool.Pool
	httpServer    *http.Server
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	gin.SetMode(cfg.GinMode)
	if err := platformauth.ValidateEnvironment(); err != nil {
		return nil, err
	}

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	verifier, err := identity.FromEnvironment()
	if err != nil {
		db.Close()
		return nil, err
	}
	var privateWriter *pgxpool.Pool
	if cfg.PrivateListPolicy.Configured() {
		poolConfig, e := pgxpool.ParseConfig(cfg.PrivateListDatabaseURL)
		if e != nil {
			db.Close()
			return nil, fmt.Errorf("invalid private-list database configuration")
		}
		poolConfig.MaxConns = 2
		poolConfig.MinConns = 0
		poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
		poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
		privateWriter, e = pgxpool.NewWithConfig(context.Background(), poolConfig)
		if e != nil {
			db.Close()
			return nil, fmt.Errorf("private-list database unavailable")
		}
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		e = privatelists.CheckDatabaseBinding(check, db, privateWriter)
		if e == nil {
			var contract bool
			e = privateWriter.QueryRow(check, `SELECT CASE WHEN to_regprocedure('identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean)') IS NULL THEN false ELSE has_function_privilege(current_user,'identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean)','EXECUTE') END`).Scan(&contract)
			if e == nil && !contract {
				e = fmt.Errorf("auth owner role contract unavailable")
			}
		}
		cancel()
		if e != nil {
			privateWriter.Close()
			db.Close()
			return nil, fmt.Errorf("private-list writer must use the same migrated logical application database")
		}
	}
	router := httpapi.NewRouter(logger, db, httpapi.RouterConfig{
		Verifier:          verifier,
		RemoteProvider:    cfg.RemoteProvider,
		PrivateListWriter: privateWriter,
		PrivateListPolicy: cfg.PrivateListPolicy,

		InvestigatorVerifier: verifier,

		ScreeningProviderURL:   cfg.ScreeningProviderURL,
		ScreeningProviderURLs:  cfg.ScreeningProviderURLs,
		OpenSanctionsAPIHost:   cfg.OpenSanctionsAPIHost,
		OpenSanctionsAuthMode:  cfg.OpenSanctionsAuthMode,
		OpenSanctionsAPIKey:    cfg.OpenSanctionsAPIKey,
		OpenSanctionsScope:     cfg.OpenSanctionsScope,
		OpenSanctionsAlgorithm: cfg.OpenSanctionsAlgorithm,
		IngestionServiceURL:    cfg.IngestionServiceURL,
		InboxServiceURL:        cfg.InboxServiceURL,
		CaseServiceURL:         cfg.CaseServiceURL,
		BlobServiceURL:         cfg.BlobServiceURL,
		DecisionEngineURL:      cfg.DecisionEngineURL,
		HTTPClientTimeout:      cfg.HTTPClientTimeout,
		ScreeningQueueName:     cfg.ScreeningQueueName,
		ScreeningQueueWorkers:  cfg.ScreeningQueueWorkers,
		DatasetJobQueueName:    cfg.DatasetJobQueueName,
		DatasetJobQueueWorkers: cfg.DatasetJobQueueWorkers,
		MonitoredQueueName:     cfg.MonitoredQueueName,
		MonitoredQueueWorkers:  cfg.MonitoredQueueWorkers,
	})
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &App{
		cfg:           cfg,
		logger:        logger,
		db:            db,
		privateWriter: privateWriter,
		httpServer:    server,
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("starting screening service", "port", a.cfg.Port)
	if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (a *App) Close() {
	if a.privateWriter != nil {
		a.privateWriter.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}
