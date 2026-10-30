package mysql

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// FindRecordKey consumes the staged data-model v005 lookup capability. Caller
// authorization, READ COMMITTED and the publication barrier must already be held.
// Mutations additionally hold their normalized-key guard; read-only lookup does
// not allocate a key or need an absent-key lock. A missing routine is an error, not an
// absent record. This never allocates a dictionary key or accesses its tables.
func FindRecordKey(ctx context.Context, tx *sql.Tx, database string, tenant, table uuid.UUID, objectID string) (uuid.UUID, bool, error) {
	if tx == nil || tenant == uuid.Nil || table == uuid.Nil || objectID == "" || len(objectID) > 1<<20 || !utf8.ValidString(objectID) {
		return uuid.Nil, false, errors.New("bounded authorized record lookup required")
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return uuid.Nil, false, err
	}
	var key mysqlstore.NullUUID
	if err := tx.QueryRowContext(ctx, "SELECT "+schema+".data_model_find_record_object_key_v1(?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), []byte(objectID)).Scan(&key); err != nil {
		return uuid.Nil, false, err
	}
	if !key.Valid {
		return uuid.Nil, false, nil
	}
	if uuid.UUID(key.UUID) == uuid.Nil {
		return uuid.Nil, false, errors.New("invalid retained record key")
	}
	return uuid.UUID(key.UUID), true, nil
}
