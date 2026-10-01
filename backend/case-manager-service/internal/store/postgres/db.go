package postgres

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryable interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

func poolConfig(databaseURL string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	max := 8
	if raw := os.Getenv("DB_POOL_MAX_CONNS"); raw != "" {
		max, err = strconv.Atoi(raw)
		if err != nil || max < 1 || max > 100 {
			return nil, fmt.Errorf("DB_POOL_MAX_CONNS must be between 1 and 100 within the shared database budget")
		}
	}
	cfg.MaxConns = int32(max)
	cfg.MinConns = 0
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnLifetimeJitter = 5 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["application_name"] = "case-manager"
	for name, setting := range map[string]struct {
		env      string
		fallback time.Duration
	}{"statement_timeout": {"DB_STATEMENT_TIMEOUT", 10 * time.Second}, "lock_timeout": {"DB_LOCK_TIMEOUT", 3 * time.Second}, "idle_in_transaction_session_timeout": {"DB_IDLE_TRANSACTION_TIMEOUT", 15 * time.Second}} {
		value := setting.fallback
		if raw := os.Getenv(setting.env); raw != "" {
			value, err = time.ParseDuration(raw)
			if err != nil || value < time.Millisecond || value > time.Minute {
				return nil, fmt.Errorf("%s must be between 1ms and 1m", setting.env)
			}
		}
		cfg.ConnConfig.RuntimeParams[name] = strconv.FormatInt(value.Milliseconds(), 10)
	}
	return cfg, nil
}
