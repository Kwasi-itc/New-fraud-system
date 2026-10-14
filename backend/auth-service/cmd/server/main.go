package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		slog.Error("auth startup or shutdown failed", "error", err.Error())
		os.Exit(1)
	}
}
func run() error {
	defaultTenant, err := parseDefaultTenant(os.Getenv("AUTH_DEFAULT_TENANT_ID"))
	if err != nil {
		return err
	}
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("invalid database configuration")
	}
	cfg.MaxConns = 5
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("database unavailable")
	}
	defer db.Close()
	if len(os.Getenv("AUTH_PROXY_TOKEN")) < 32 || os.Getenv("PLATFORM_JWT_ISSUER") == "" || os.Getenv("PLATFORM_JWT_AUDIENCE") == "" || os.Getenv("PLATFORM_JWT_KEY_ID") == "" {
		return errors.New("proxy and platform JWT settings required")
	}
	key, err := platform.SigningKey(os.Getenv("PLATFORM_JWT_PRIVATE_KEY_FILE"))
	if err != nil {
		return err
	}
	provider, err := oidcprovider.New(ctx, oidcprovider.FromEnvironment(os.Getenv))
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	app := platform.Server{Store: platform.Store{DB: db}, Provider: provider, ProxyToken: os.Getenv("AUTH_PROXY_TOKEN"), Key: key, KeyID: os.Getenv("PLATFORM_JWT_KEY_ID"), Issuer: os.Getenv("PLATFORM_JWT_ISSUER"), Audience: os.Getenv("PLATFORM_JWT_AUDIENCE"), Logger: logger}
	app.DefaultTenantID = defaultTenant
	if os.Getenv("AUTH_INVITATIONS_ENABLED") == "true" {
		invitations, e := platform.NewInvitations(app.Store, provider.Issuer(), os.Getenv("AUTH_APP_ORIGIN"), os.Getenv("AUTH_INVITATION_KEY"))
		if e != nil {
			return e
		}
		mailer, e := platform.SMTPFromEnvironment(os.Getenv)
		if e != nil {
			return e
		}
		app.Invitations = invitations
		go invitations.RunDelivery(ctx, mailer)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8087"
	}
	server := &http.Server{Addr: ":" + port, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("auth service started", "port", port)
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("auth HTTP server failed")
		}
	case <-ctx.Done():
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(closeCtx)
}

func parseDefaultTenant(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, nil
	}
	tenant, err := uuid.Parse(value)
	if err != nil || tenant == uuid.Nil {
		return uuid.Nil, errors.New("AUTH_DEFAULT_TENANT_ID must be a nonzero tenant UUID")
	}
	return tenant, nil
}
