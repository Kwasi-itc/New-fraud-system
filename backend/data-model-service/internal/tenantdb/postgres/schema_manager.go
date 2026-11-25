package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type SchemaManager struct {
	db   executor
	pool *pgxpool.Pool
}

func NewSchemaManager(db executor) SchemaManager {
	pool, _ := db.(*pgxpool.Pool)
	return SchemaManager{db: db, pool: pool}
}

func (m SchemaManager) ProvisionTenantSchema(ctx context.Context, record tenant.Tenant) error {
	query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", sanitizeIdentifier(record.SchemaName))
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	return nil
}
