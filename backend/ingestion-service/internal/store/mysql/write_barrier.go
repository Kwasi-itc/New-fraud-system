package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrWriteUnavailable = errors.New("ingestion physical publication unavailable or stale")
var ErrWriteMapping = errors.New("ingestion physical mapping invalid or exceeds bounds")

type WriteField struct {
	ID         uuid.UUID
	Type       string
	Archived   bool
	Components map[string]string
}

// WriteSnapshot is a copied description, not a reusable authorization token.
// It is valid only while the caller keeps the acquiring transaction open.
type WriteSnapshot struct {
	TenantID, TableID, Version uuid.UUID
	Epoch                      int64
	PhysicalName               string
	Fields                     []WriteField
}

// LockWriteSnapshot consumes data-model's documented registry contract. The
// owner must authorize the tenant/table before calling, use READ COMMITTED, and
// keep this transaction through all physical data/audit/outbox/receipt effects.
// The shared exact-table lock prevents DDL preparation from closing the gate
// until this transaction ends. No network calls are allowed while it is held.
// This function neither opens a gate nor verifies/installs physical DDL itself.
func LockWriteSnapshot(ctx context.Context, tx *sql.Tx, database string, tenant, table, expected uuid.UUID, maxFields int) (WriteSnapshot, error) {
	var zero WriteSnapshot
	if tx == nil || tenant == uuid.Nil || table == uuid.Nil || expected == uuid.Nil || maxFields < 2 || maxFields > 1000 {
		return zero, errors.New("authorized write scope, version and field bounds required")
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return zero, err
	}
	var name, state string
	var version, operation mysqlstore.NullUUID
	var epoch int64
	err = tx.QueryRowContext(ctx, "SELECT physical_name,published_version,epoch,write_state,active_operation FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&name, &version, &epoch, &state, &operation)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, ErrWriteUnavailable
	}
	if err != nil {
		return zero, err
	}
	if state != "open" || !version.Valid || uuid.UUID(version.UUID) != expected || operation.Valid || epoch < 0 {
		return zero, ErrWriteUnavailable
	}
	if !registryName(name, "tenant_", table, "", 63) {
		return zero, ErrWriteMapping
	}
	var archived bool
	if err := tx.QueryRowContext(ctx, "SELECT archived FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, ErrWriteUnavailable
		}
		return zero, err
	}
	if archived {
		return zero, ErrWriteUnavailable
	}
	// Include fields with missing mappings, so partial registration cannot pass.
	rows, err := tx.QueryContext(ctx, "SELECT f.id,f.data_type,f.archived,p.component,p.physical_name FROM "+schema+".data_model_fields f LEFT JOIN "+schema+".data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.tenant_id=? AND f.table_id=? ORDER BY f.id,p.component LIMIT ?", mysqlstore.UUID(tenant), mysqlstore.UUID(table), maxFields*3+1)
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	result := WriteSnapshot{TenantID: tenant, TableID: table, Version: expected, Epoch: epoch, PhysicalName: name}
	count := 0
	used := make(map[string]bool)
	for rows.Next() {
		count++
		if count > maxFields*3 {
			return zero, ErrWriteMapping
		}
		var id mysqlstore.UUID
		var typ string
		var archived bool
		var component, physical sql.NullString
		if err := rows.Scan(&id, &typ, &archived, &component, &physical); err != nil {
			return zero, err
		}
		fieldID := uuid.UUID(id)
		if fieldID == uuid.Nil || !component.Valid || !physical.Valid || !registryName(physical.String, "field_", fieldID, "_"+component.String, 59) || used[physical.String] {
			return zero, ErrWriteMapping
		}
		used[physical.String] = true
		if len(result.Fields) == 0 || result.Fields[len(result.Fields)-1].ID != fieldID {
			if len(result.Fields) == maxFields {
				return zero, ErrWriteMapping
			}
			result.Fields = append(result.Fields, WriteField{ID: fieldID, Type: typ, Archived: archived, Components: make(map[string]string)})
		}
		field := &result.Fields[len(result.Fields)-1]
		if _, exists := field.Components[component.String]; exists {
			return zero, ErrWriteMapping
		}
		field.Components[component.String] = physical.String
	}
	if err := rows.Err(); err != nil {
		return zero, err
	}
	if err := rows.Close(); err != nil {
		return zero, err
	}
	if len(result.Fields) < 2 {
		return zero, ErrWriteMapping
	}
	for _, field := range result.Fields {
		want := []string{"value"}
		switch field.Type {
		case "bool", "int", "float", "string", "timestamp":
		case "ip_address":
			want = []string{"address", "family", "prefix"}
		default:
			return zero, ErrWriteMapping
		}
		if len(field.Components) != len(want) {
			return zero, ErrWriteMapping
		}
		for _, part := range want {
			if field.Components[part] == "" {
				return zero, ErrWriteMapping
			}
		}
	}
	return result, nil
}

func registryName(name, prefix string, id uuid.UUID, component string, maxBytes int) bool {
	if len(name) > maxBytes || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, "_"+strings.ReplaceAll(id.String(), "-", "")+component) {
		return false
	}
	for _, c := range name {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
