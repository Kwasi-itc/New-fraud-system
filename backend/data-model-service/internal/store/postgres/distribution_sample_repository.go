package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type DistributionSampleRepository struct{ db executor }

func NewDistributionSampleRepository(db executor) DistributionSampleRepository {
	return DistributionSampleRepository{db: db}
}

func (r DistributionSampleRepository) ReadValues(ctx context.Context, tenantID uuid.UUID, tableName, fieldName string, limit int) ([]*string, bool, error) {
	var schemaName string
	err := r.db.QueryRow(ctx, `
		SELECT t.schema_name
		FROM core.tenants t
		JOIN core.model_tables mt ON mt.tenant_id = t.id AND mt.name = $2 AND mt.archived = false
		JOIN core.model_fields mf ON mf.table_id = mt.id AND mf.name = $3 AND mf.archived = false
		WHERE t.id = $1
	`, tenantID, tableName, fieldName).Scan(&schemaName)
	if err != nil {
		return nil, false, fmt.Errorf("resolve stored distribution field: %w", err)
	}
	quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	query := fmt.Sprintf(`SELECT %s::text FROM %s.%s LIMIT $1`, quote(fieldName), quote(schemaName), quote(tableName))
	rows, err := r.db.Query(ctx, query, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("sample stored distribution: %w", err)
	}
	defer rows.Close()
	values := make([]*string, 0, limit)
	truncated := false
	for rows.Next() {
		var value *string
		if err := rows.Scan(&value); err != nil {
			return nil, false, err
		}
		if len(values) == limit {
			truncated = true
			break
		}
		values = append(values, value)
	}
	return values, truncated, rows.Err()
}
