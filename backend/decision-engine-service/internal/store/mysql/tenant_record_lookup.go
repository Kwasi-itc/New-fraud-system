package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Resolve through the owner's registered index and read-only key function. An
// absent dictionary binding is a confirmed miss, not permission to scan raw
// LONGTEXT or allocate a key. The data query uses its unique BINARY(16) index and
// retains a full exact-text residual even after resolving a colliding bucket.
func (r authoringRepository) recordLookup(ctx context.Context, t registryTable, value string) (string, []any, error) {
	if t.ownerRecordKeys {
		// registry() already locked and verified the actual owner publication
		// token and physical mappings. v005 only resolves the scoped exact key;
		// the physical row read retains the full binary-text residual.
		lookup, ok := t.fields[t.lookup]
		if !ok || !lookup.IsUnique || lookup.Type != "string" {
			return "", nil, ErrPublishedModelUnavailable
		}
		var key mysqlstore.NullUUID
		err := r.tx.QueryRowContext(ctx, "SELECT "+r.schema+".data_model_find_record_object_key_v1(?,?,?)", mysqlstore.UUID(r.tenant), mysqlstore.UUID(t.id), mysqlstore.Text(value)).Scan(&key)
		if err != nil {
			return "", nil, errors.Join(ErrPublishedModelUnavailable, err)
		}
		if !key.Valid {
			return "", nil, sql.ErrNoRows
		}
		if uuid.UUID(key.UUID) == uuid.Nil {
			return "", nil, ErrPublishedModelUnavailable
		}
		return "`record_object_key_id`=? AND CAST(" + lookup.columns["value"] + " AS BINARY)=CAST(? AS BINARY)", []any{key.UUID, mysqlstore.Text(value)}, nil
	}
	var field mysqlstore.UUID
	var kind, keyColumn string
	args := []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(t.id)}
	if err := r.tx.QueryRowContext(ctx, "SELECT field_id,lookup_kind,physical_key_column FROM "+r.schema+".data_model_physical_lookups WHERE tenant_id=? AND table_id=?", args...).Scan(&field, &kind, &keyColumn); err != nil {
		return "", nil, errors.Join(ErrPublishedModelUnavailable, err)
	}
	lookup, ok := t.fields[t.lookup]
	if !ok || uuid.UUID(field) != lookup.ID || kind != "object_id_exact_v1" {
		return "", nil, ErrPublishedModelUnavailable
	}
	column, err := mysqlstore.Identifier(keyColumn)
	if err != nil {
		return "", nil, ErrPublishedModelUnavailable
	}
	var key mysqlstore.NullUUID
	if err := r.tx.QueryRowContext(ctx, "SELECT "+r.schema+".data_model_record_lookup_v1(?,?,?,?)", args[0], args[1], mysqlstore.UUID(t.version), mysqlstore.Text(value)).Scan(&key); err != nil {
		return "", nil, errors.Join(ErrPublishedModelUnavailable, err)
	}
	if !key.Valid {
		return "", nil, sql.ErrNoRows
	}
	if uuid.UUID(key.UUID) == uuid.Nil {
		return "", nil, ErrPublishedModelUnavailable
	}
	return column + "=? AND CAST(" + lookup.columns["value"] + " AS BINARY)=CAST(? AS BINARY)", []any{key.UUID, mysqlstore.Text(value)}, nil
}
