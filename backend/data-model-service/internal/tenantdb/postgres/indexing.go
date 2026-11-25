package postgres

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type IndexJobMetrics struct {
	IndexJobDurationSecondsTotal float64 `json:"index_job_duration_seconds_total"`
	IndexJobInvalidCleanupTotal  uint64  `json:"index_job_invalid_cleanup_total"`
}

var (
	indexJobDurationNanosTotal  atomic.Int64
	indexJobInvalidCleanupTotal atomic.Uint64
)

func IndexJobMetricsSnapshot() IndexJobMetrics {
	return IndexJobMetrics{
		IndexJobDurationSecondsTotal: float64(indexJobDurationNanosTotal.Load()) / 1e9,
		IndexJobInvalidCleanupTotal:  indexJobInvalidCleanupTotal.Load(),
	}
}

func (m SchemaManager) CreateTable(ctx context.Context, record tenant.Tenant, table datamodel.Table) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id UUID NOT NULL PRIMARY KEY,
		object_id TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL,
		valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY'
	) WITH (
		autovacuum_vacuum_scale_factor = 0.02,
		autovacuum_analyze_scale_factor = 0.01
	)`, sanitizeIdentifier(record.SchemaName, table.Name))
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("create tenant table: %w", err)
	}
	return nil
}

func (m SchemaManager) DropTable(ctx context.Context, record tenant.Tenant, table datamodel.Table) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", sanitizeIdentifier(record.SchemaName, table.Name))
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("drop tenant table: %w", err)
	}
	return nil
}

func (m SchemaManager) AddField(ctx context.Context, record tenant.Tenant, table datamodel.Table, field datamodel.Field) error {
	dataType, err := toPgType(field.DataType)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		sanitizeIdentifier(record.SchemaName, table.Name),
		sanitizeIdentifier(field.Name),
		dataType,
	)
	if _, err = m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("add tenant field: %w", err)
	}
	return nil
}

func (m SchemaManager) DropField(ctx context.Context, record tenant.Tenant, table datamodel.Table, field datamodel.Field) error {
	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s",
		sanitizeIdentifier(record.SchemaName, table.Name),
		sanitizeIdentifier(field.Name),
	)
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("drop tenant field: %w", err)
	}
	return nil
}

func (m SchemaManager) ArchiveField(ctx context.Context, record tenant.Tenant, table datamodel.Table, field datamodel.Field) error {
	newName := archivedName(field.Name)
	query := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
		sanitizeIdentifier(record.SchemaName, table.Name),
		sanitizeIdentifier(field.Name),
		sanitizeIdentifier(newName),
	)
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("archive tenant field: %w", err)
	}
	return nil
}

func (m SchemaManager) CreateUniqueIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, columns []string) error {
	return m.createIndex(ctx, record, table, columns, true)
}

func (m SchemaManager) DropUniqueIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, columns []string) error {
	query := fmt.Sprintf("DROP INDEX IF EXISTS %s", sanitizeIdentifier(record.SchemaName, managedIndexName(table.Name, columns, true)))
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("drop unique index: %w", err)
	}
	return nil
}

func (m SchemaManager) CreateManagedIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, job datamodel.IndexJob) error {
	return m.createCoveringIndex(ctx, record, table, job.Columns, job.IncludeColumns, false)
}

func (m SchemaManager) DropManagedIndex(ctx context.Context, record tenant.Tenant, indexName string) error {
	dropSQL := fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s", sanitizeIdentifier(record.SchemaName, indexName))
	if m.pool == nil {
		dropSQL = fmt.Sprintf("DROP INDEX IF EXISTS %s", sanitizeIdentifier(record.SchemaName, indexName))
		_, err := m.db.Exec(ctx, dropSQL)
		return err
	}
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire pinned connection for concurrent drop: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop index concurrently: %w", err)
	}
	return nil
}

func (m SchemaManager) createIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, columns []string, unique bool) error {
	return m.createCoveringIndex(ctx, record, table, columns, nil, unique)
}

func (m SchemaManager) createCoveringIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, columns, includeColumns []string, unique bool) error {
	indexName := managedIndexNameWithIncludes(table.Name, columns, includeColumns, unique)
	ddl := buildCreateIndexDDL(record.SchemaName, table.Name, indexName, columns, includeColumns, unique, m.pool != nil)

	if m.pool != nil {
		return m.execConcurrentIndexBuild(ctx, record, table, indexName, ddl)
	}
	if _, err := m.db.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	return nil
}

func buildCreateIndexDDL(schemaName, tableName, indexName string, columns, includeColumns []string, unique, concurrently bool) string {
	indexColumns := make([]string, len(columns))
	for i, column := range columns {
		indexColumns[i] = sanitizeIdentifier(column)
	}
	modifier := ""
	if unique {
		modifier = "UNIQUE "
	}
	concurrentClause := ""
	if concurrently {
		concurrentClause = "CONCURRENTLY "
	} else {
		concurrentClause = "IF NOT EXISTS "
	}
	includeClause := ""
	if len(includeColumns) > 0 {
		inc := make([]string, len(includeColumns))
		for i, col := range includeColumns {
			inc[i] = sanitizeIdentifier(col)
		}
		includeClause = " INCLUDE (" + strings.Join(inc, ", ") + ")"
	}
	return fmt.Sprintf("CREATE %sINDEX %s%s ON %s (%s)%s",
		modifier,
		concurrentClause,
		sanitizeIdentifier(indexName),
		sanitizeIdentifier(schemaName, tableName),
		strings.Join(indexColumns, ", "),
		includeClause,
	)
}

func (m SchemaManager) execConcurrentIndexBuild(ctx context.Context, record tenant.Tenant, table datamodel.Table, indexName, ddl string) error {
	started := time.Now()
	defer func() {
		indexJobDurationNanosTotal.Add(time.Since(started).Nanoseconds())
	}()
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire pinned connection for concurrent index: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			conn.Release()
		}
	}()

	if txStatus := conn.Conn().PgConn().TxStatus(); txStatus != 'I' {
		return fmt.Errorf("concurrent index requires idle non-transactional connection, got tx status %q", txStatus)
	}

	if _, err := conn.Exec(ctx, "SET lock_timeout = '5s'"); err != nil {
		return fmt.Errorf("set lock_timeout: %w", err)
	}
	defer func() {
		if _, resetErr := conn.Exec(context.Background(), "RESET lock_timeout"); resetErr != nil {
			_ = conn.Conn().Close(context.Background())
			closed = true
		}
	}()

	state, err := m.GetManagedIndexState(ctx, record, table, datamodel.IndexJob{Columns: nil})
	_ = state // re-check exact indexName below via pg_index
	var exists, valid bool
	if err := conn.QueryRow(ctx, `
		SELECT
			EXISTS (
				SELECT 1 FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'i'
			),
			EXISTS (
				SELECT 1 FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				JOIN pg_index i ON i.indexrelid = c.oid
				WHERE n.nspname = $1 AND c.relname = $2 AND i.indisvalid AND i.indisready
			)
	`, record.SchemaName, indexName).Scan(&exists, &valid); err != nil {
		return fmt.Errorf("inspect index state before concurrent build: %w", err)
	}
	if exists && valid {
		return nil
	}
	if exists && !valid {
		indexJobInvalidCleanupTotal.Add(1)
		dropSQL := fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s", sanitizeIdentifier(record.SchemaName, indexName))
		if _, err := conn.Exec(ctx, dropSQL); err != nil {
			return fmt.Errorf("drop invalid leftover index %s: %w", indexName, err)
		}
	}

	if _, err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create index concurrently: %w", err)
	}

	var validAfter bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_index i ON i.indexrelid = c.oid
			WHERE n.nspname = $1 AND c.relname = $2 AND i.indisvalid AND i.indisready
		)
	`, record.SchemaName, indexName).Scan(&validAfter); err != nil {
		return fmt.Errorf("verify concurrent index validity: %w", err)
	}
	if !validAfter {
		return fmt.Errorf("index %s.%s built but pg_index reports invalid", record.SchemaName, indexName)
	}
	return nil
}

func toPgType(dataType datamodel.DataType) (string, error) {
	switch dataType {
	case datamodel.DataTypeBool:
		return "BOOLEAN", nil
	case datamodel.DataTypeInt:
		return "INTEGER", nil
	case datamodel.DataTypeFloat:
		return "DOUBLE PRECISION", nil
	case datamodel.DataTypeString:
		return "TEXT", nil
	case datamodel.DataTypeTimestamp:
		return "TIMESTAMPTZ", nil
	case datamodel.DataTypeIPAddress:
		return "INET", nil
	default:
		return "", fmt.Errorf("unsupported data type: %s", dataType)
	}
}

func managedIndexName(tableName string, columns []string, unique bool) string {
	return managedIndexNameWithIncludes(tableName, columns, nil, unique)
}

func managedIndexNameWithIncludes(tableName string, columns, includeColumns []string, unique bool) string {
	prefix := "idx"
	if unique {
		prefix = "uniq"
	}
	var key string
	if len(includeColumns) == 0 {
		key = tableName + "_" + strings.Join(columns, "_")
	} else {
		key = tableName + "|" + strings.Join(columns, ",") + "|inc|" + strings.Join(includeColumns, ",")
	}
	sum := sha1.Sum([]byte(key))
	shortTable := tableName
	if len(shortTable) > 32 {
		shortTable = shortTable[:32]
	}
	return fmt.Sprintf("%s_%s_%s", prefix, shortTable, hex.EncodeToString(sum[:4]))
}

func archivedName(name string) string {
	sum := sha1.Sum([]byte(name))
	return fmt.Sprintf("old_%s_%s", name, hex.EncodeToString(sum[:3]))
}
