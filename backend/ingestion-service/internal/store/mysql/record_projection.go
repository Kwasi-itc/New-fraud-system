package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

var ErrRecordProjection = errors.New("bounded active mapped record fields required")

type projectionField struct {
	definition ingestion.FieldSchema
	width      int
	column     string
	components []string
}

// RecordProjection is transaction-local, not an authorization or cache token.
// Its SQL contains only validated physical identifiers, never logical aliases.
type RecordProjection struct {
	table, list     string
	objectColumn    string
	database        string
	tenant, tableID uuid.UUID
	fields          []projectionField
	columns         int
}

// NewRecordProjection requires owner authorization before entry and an existing
// READ COMMITTED transaction held through execution. Reuse the validated writer
// mapping/barrier contract: the conservative active/writable/open gate applies.
// A less restrictive read policy must be introduced explicitly, not inferred here.
func NewRecordProjection(ctx context.Context, tx *sql.Tx, database string, tenant uuid.UUID, model ingestion.PublishedDataModel, objectType string, expected uuid.UUID, names []string) (*RecordProjection, error) {
	if len(names) < 1 || len(names) > 100 {
		return nil, ErrRecordProjection
	}
	table, ok := model.Tables[objectType]
	if !ok {
		return nil, ErrRecordProjection
	}
	requested := append([]string(nil), names...)
	seen := make(map[string]bool, len(requested))
	for _, name := range requested {
		field, ok := table.Fields[name]
		if name == "" || seen[name] || !ok || field.Archived || field.Name != name {
			return nil, ErrRecordProjection
		}
		seen[name] = true
	}
	// NewRecordWriter binds tenant/table/publication and verifies the complete
	// field-ID/type/component mapping under the existing shared registry lock.
	mapped, err := NewRecordWriter(ctx, tx, database, tenant, model, objectType, expected)
	if err != nil {
		return nil, err
	}
	p := &RecordProjection{database: database, tenant: tenant, tableID: table.ID, table: mapped.qualified, objectColumn: mapped.objectColumn, fields: make([]projectionField, 0, len(requested))}
	var columns []string
	for _, name := range requested {
		field := mapped.fields[name]
		parts := []string{"value"}
		if field.definition.DataType == "ip_address" {
			parts = []string{"address", "family", "prefix"}
		}
		for _, part := range parts {
			columns = append(columns, "`"+field.physical.Components[part]+"`")
		}
		components := append([]string(nil), columns[len(columns)-len(parts):]...)
		p.fields = append(p.fields, projectionField{definition: field.definition, width: len(parts), column: components[0], components: components})
	}
	p.columns = len(columns)
	p.list = strings.Join(columns, ",")
	return p, nil
}

// SQL returns a projection and qualified table separately, without FROM/WHERE,
// aliases, filters or pagination. Callers must bind their predicate values and
// bound transfer/row counts. In particular, this fragment alone does not limit
// LONGTEXT transfer; IN-01.3.021 must supply before-Scan byte guards.
func (p *RecordProjection) SQL() (columns, table string) { return p.list, p.table }
func (p *RecordProjection) ColumnCount() int             { return p.columns }

// Decode names results from the copied logical schema, not SQL aliases. Each
// field uses the strict typed decoder. A failure never returns a partial record.
// maxStringBytes is a per-field decoding limit, not a combined row/page budget.
func (p *RecordProjection) Decode(values []any, maxStringBytes int) (map[string]any, error) {
	if p == nil || p.columns < 1 || len(p.fields) < 1 || len(values) != p.columns {
		return nil, ErrRecordProjection
	}
	result := make(map[string]any, len(p.fields))
	offset := 0
	for _, field := range p.fields {
		value, err := DecodeRecordValue(field.definition, values[offset:offset+field.width], maxStringBytes)
		if err != nil {
			return nil, err
		}
		result[field.definition.Name] = value
		offset += field.width
	}
	return result, nil
}
