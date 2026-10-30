package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// LookupGuardSchema is staged owner DDL. The composite FK binds guards to the
// data-model registry in the shared database; it grants no physical readiness.
func LookupGuardSchema(database string) (string, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "CREATE TABLE " + schema + ".ingestion_lookup_guards(tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,key_digest BINARY(32) NOT NULL,PRIMARY KEY(tenant_id,table_id,key_digest),FOREIGN KEY(tenant_id,table_id) REFERENCES " + schema + ".data_model_physical_tables(tenant_id,table_id) ON DELETE RESTRICT) ENGINE=InnoDB", nil
}

// LockLookupKeys reserves missing or existing normalized object IDs after the
// owner's publication lock and before physical row locks. One ordered multi-row
// INSERT locks persistent digest buckets until the outer transaction ends.
// Hash collisions ONLY serialize unrelated keys; a digest is never record
// identity. Physical lookup must still compare full values through an approved
// data-model lookup contract. No generic upsert of a physical record occurs here.
// The owner must acquire the complete batch once, then keep effects/result order.
func LockLookupKeys(ctx context.Context, tx *sql.Tx, database string, tenant, table uuid.UUID, keys []string, maxKeyBytes, maxBatchBytes int) error {
	return lockLookupKeys(ctx, tx, database, tenant, table, keys, maxKeyBytes, maxBatchBytes, sha256.Sum256)
}

func lockLookupKeys(ctx context.Context, tx *sql.Tx, database string, tenant, table uuid.UUID, keys []string, maxKeyBytes, maxBatchBytes int, digest func([]byte) [32]byte) error {
	if tx == nil || tenant == uuid.Nil || table == uuid.Nil || len(keys) < 1 || len(keys) > 500 || maxKeyBytes < 1 || maxKeyBytes > 1<<20 || maxBatchBytes < 1 || maxBatchBytes > 16<<20 {
		return errors.New("bounded authorized lookup guard scope required")
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(keys))
	buckets := make(map[[32]byte]bool, len(keys))
	ordered := make([][32]byte, 0, len(keys))
	remaining := maxBatchBytes
	for _, key := range keys {
		if key == "" || len(key) > maxKeyBytes || len(key) > remaining || !utf8.ValidString(key) || seen[key] {
			return errors.New("invalid, duplicate or oversized normalized lookup key")
		}
		remaining -= len(key)
		seen[key] = true
		hash := digest([]byte(key))
		if !buckets[hash] {
			buckets[hash] = true
			ordered = append(ordered, hash)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i][:], ordered[j][:]) < 0 })
	args := make([]any, 0, len(ordered)*3)
	for i := range ordered {
		args = append(args, mysqlstore.UUID(tenant), mysqlstore.UUID(table), ordered[i][:])
	}
	values := strings.TrimSuffix(strings.Repeat("(?,?,?),", len(ordered)), ",")
	_, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".ingestion_lookup_guards(tenant_id,table_id,key_digest) VALUES "+values+" ON DUPLICATE KEY UPDATE key_digest=key_digest", args...)
	return err
}
