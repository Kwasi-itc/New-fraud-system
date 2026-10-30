package mysql

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrRecordLookup = errors.New("bounded exact object identity required")

// ReadExactRecord requires prior owner authorization and a READ COMMITTED
// transaction retained until collection completes. It neither commits nor writes.
// Mapping publication is locked by NewRecordProjection. Resolve the immutable
// full-byte dictionary identity first, then bind the unique key. A routine in the
// predicate prevents MySQL index access. These READ COMMITTED statements may see
// concurrent row changes; they never reuse a mutable or cross-tenant key mapping.
// Missing records return sql.ErrNoRows, never a successful empty object.
func ReadExactRecord(ctx context.Context, tx *sql.Tx, database string, tenant uuid.UUID, model ingestion.PublishedDataModel, objectType string, expected uuid.UUID, names []string, objectID string, limits RecordResultLimits) (map[string]any, error) {
	if tx == nil || tenant == uuid.Nil || expected == uuid.Nil || objectID == "" || len(objectID) > 1<<20 || !utf8.ValidString(objectID) {
		return nil, ErrRecordLookup
	}
	p, err := NewRecordProjection(ctx, tx, database, tenant, model, objectType, expected, names)
	if err != nil {
		return nil, err
	}
	key, found, err := FindRecordKey(ctx, tx, database, tenant, model.Tables[objectType].ID, objectID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, sql.ErrNoRows
	}
	query, args, b, err := p.compileExactRead(key, objectID, limits)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	records, err := b.Collect(rows)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, sql.ErrNoRows
	}
	return records[0], nil
}

func (p *RecordProjection) compileExactRead(key uuid.UUID, objectID string, limits RecordResultLimits) (string, []any, *BoundedRecordProjection, error) {
	if key == uuid.Nil || objectID == "" || len(objectID) > 1<<20 || !utf8.ValidString(objectID) {
		return "", nil, nil, ErrRecordLookup
	}
	b, err := p.Bounded(limits)
	if err != nil {
		return "", nil, nil, err
	}
	columns, table, args := b.SQL()
	args = append(args, mysqlstore.UUID(key), []byte(objectID))
	query := "SELECT " + columns + " FROM " + table + " WHERE record_object_key_id=? AND CAST(" + p.objectColumn + " AS BINARY)=? LIMIT 1"
	if err := ValidateRecordQuery(query, args, RecordQueryLimits{Parameters: 102, ParameterBytes: 2 << 20, SQLBytes: 1 << 20}); err != nil {
		return "", nil, nil, err
	}
	return query, args, b, nil
}
