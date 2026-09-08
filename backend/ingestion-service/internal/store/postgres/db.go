package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

func NewPool(ctx context.Context, databaseURL string, poolCfg PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pg config: %w", err)
	}
	if poolCfg.MaxConns > 0 {
		cfg.MaxConns = poolCfg.MaxConns
	}
	if poolCfg.MinConns > 0 {
		cfg.MinConns = poolCfg.MinConns
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
