package postgres

import (
	"context"
	"fmt"
	"os"
	"strconv"

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
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if raw := os.Getenv("DATABASE_MAX_CONNS"); raw != "" {
		max, err := strconv.Atoi(raw)
		if err != nil || max < 1 || max > 100 {
			return nil, fmt.Errorf("DATABASE_MAX_CONNS must be 1..100")
		}
		cfg.MaxConns = int32(max)
		if cfg.MinConns > cfg.MaxConns {
			return nil, fmt.Errorf("pool minimum exceeds DATABASE_MAX_CONNS")
		}
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
