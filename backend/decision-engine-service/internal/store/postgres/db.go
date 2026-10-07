package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Zero leaves the corresponding connection-string/pgx default unchanged.
type PoolConfig struct {
	MaxConns int32
	MinConns int32
}

func poolConfig(databaseURL string, limits PoolConfig) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if limits.MaxConns < 0 || limits.MinConns < 0 {
		return nil, fmt.Errorf("pool limits must be nonnegative")
	}
	if limits.MaxConns > 0 {
		cfg.MaxConns = limits.MaxConns
	}
	if limits.MinConns > 0 {
		cfg.MinConns = limits.MinConns
	}
	if cfg.MaxConns <= 0 || cfg.MinConns < 0 || cfg.MinConns > cfg.MaxConns {
		return nil, fmt.Errorf("invalid effective pool limits: maximum must be positive and minimum within maximum")
	}
	return cfg, nil
}

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return NewPoolWithConfig(ctx, databaseURL, PoolConfig{})
}

func NewPoolWithConfig(ctx context.Context, databaseURL string, limits PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(databaseURL, limits)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
