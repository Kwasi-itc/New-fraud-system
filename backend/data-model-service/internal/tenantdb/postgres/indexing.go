package postgres

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

func (m SchemaManager) CreateTable(ctx context.Context, record tenant.Tenant, table datamodel.Table) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id UUID NOT NULL PRIMARY KEY,
		object_id TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL,
		valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY'
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
	return m.createIndex(ctx, record, table, job.Columns, false)
}

func (m SchemaManager) createIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, columns []string, unique bool) error {
	indexColumns := make([]string, len(columns))
	for i, column := range columns {
		indexColumns[i] = sanitizeIdentifier(column)
	}
	modifier := ""
	if unique {
		modifier = "UNIQUE "
	}
	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		modifier,
		sanitizeIdentifier(managedIndexName(table.Name, columns, unique)),
		sanitizeIdentifier(record.SchemaName, table.Name),
		strings.Join(indexColumns, ", "),
	)
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("create index: %w", err)
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
	prefix := "idx"
	if unique {
		prefix = "uniq"
	}
	key := tableName + "_" + strings.Join(columns, "_")
	sum := sha1.Sum([]byte(key))
	return fmt.Sprintf("%s_%s_%s", prefix, tableName, hex.EncodeToString(sum[:4]))
}

func archivedName(name string) string {
	sum := sha1.Sum([]byte(name))
	return fmt.Sprintf("old_%s_%s", name, hex.EncodeToString(sum[:3]))
}
