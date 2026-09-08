package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return NewPoolWithConfig(ctx, databaseURL, PoolConfig{})
}

type PoolConfig struct {
	MaxConns int32
	MinConns int32
}

type RuntimeSettings struct {
	SynchronousCommit          string
	MaxWALSize                 string
	CheckpointTimeout          string
	CheckpointCompletionTarget string
	WALCompression             string
}

func NewPoolWithConfig(ctx context.Context, databaseURL string, poolConfig PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if poolConfig.MaxConns > 0 {
		cfg.MaxConns = poolConfig.MaxConns
	}
	if poolConfig.MinConns > 0 {
		cfg.MinConns = poolConfig.MinConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pg pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pg: %w", err)
	}
	return pool, nil
}

func ReadRuntimeSettings(ctx context.Context, pool *pgxpool.Pool) (RuntimeSettings, error) {
	var settings RuntimeSettings
	err := pool.QueryRow(ctx, `
		SELECT current_setting('synchronous_commit'),
		       current_setting('max_wal_size'),
		       current_setting('checkpoint_timeout'),
		       current_setting('checkpoint_completion_target'),
		       current_setting('wal_compression')
	`).Scan(
		&settings.SynchronousCommit,
		&settings.MaxWALSize,
		&settings.CheckpointTimeout,
		&settings.CheckpointCompletionTarget,
		&settings.WALCompression,
	)
	if err != nil {
		return RuntimeSettings{}, fmt.Errorf("read postgres runtime settings: %w", err)
	}
	return settings, nil
}
