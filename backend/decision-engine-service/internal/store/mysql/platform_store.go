package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrProjectionLimit = errors.New("decision platform projection exceeds configured budget")
var ErrTenantScope = errors.New("decision platform identity or tenant scope is invalid")

// PlatformAuthorizer must validate trusted identity and tenant permission using
// local SQL on this transaction. Writes need current locked authorization. For
// reads it must respect the caller's read-only snapshot. No network calls, DDL,
// commit/rollback or retained transaction are permitted. A tenant ID is not auth.
type PlatformAuthorizer func(context.Context, *sql.Tx, uuid.UUID, bool) error

// PlatformStore is deliberately NOT a partial ports.TransactionManager. Only
// these three implemented repositories are exposed; no other operation can
// silently return nil/success. Runtime still selects PostgreSQL.
type PlatformStore struct {
	db                *sql.DB
	schema            string
	tenant            uuid.UUID
	maxRows, maxBytes int
	authorize         PlatformAuthorizer
}

type PlatformRepositories struct {
	RecordTags    RecordTagRepository
	RiskSnapshots RiskSnapshotRepository
	IPFlags       IPFlagRepository
}

func NewPlatformStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize PlatformAuthorizer) (*PlatformStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || maxRows < 1 || maxRows > 10000 || maxBytes < 1 || maxBytes > 64<<20 || authorize == nil {
		return nil, errors.New("pool, tenant, bounded row/text budgets and platform authorizer required")
	}
	return &PlatformStore{db, schema, tenant, maxRows, maxBytes, authorize}, nil
}

// Mutate commits all owner writes or returns an error. Captured callback results
// are provisional until this returns nil. CommitError needs outcome
// reconciliation with original identities; this slice has no operation receipts
// or automatic retries and must not claim exactly-once delivery.
func (s *PlatformStore) Mutate(ctx context.Context, fn func(PlatformRepositories) error) error {
	return s.run(ctx, true, fn)
}

func (s *PlatformStore) ReadSnapshot(ctx context.Context, fn func(PlatformRepositories) error) error {
	return s.run(ctx, false, fn)
}

func (s *PlatformStore) run(ctx context.Context, write bool, fn func(PlatformRepositories) error) error {
	if s == nil || s.db == nil || s.authorize == nil || fn == nil {
		return errors.New("initialized platform store and callback required")
	}
	run := mysqlstore.Snapshot
	if write {
		run = mysqlstore.Mutation
	}
	return run(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, write); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r := platformRepository{tx, s.schema, s.tenant, s.maxRows, s.maxBytes, write}
		return fn(PlatformRepositories{RecordTagRepository{r}, RiskSnapshotRepository{r}, IPFlagRepository{r}})
	})
}

type platformRepository struct {
	tx                *sql.Tx
	schema            string
	tenant            uuid.UUID
	maxRows, maxBytes int
	write             bool
}

func (r platformRepository) scope(tenant string, text ...string) error {
	id, err := uuid.Parse(tenant)
	if r.tx == nil || r.schema == "" || err != nil || id == uuid.Nil || id != r.tenant {
		return ErrTenantScope
	}
	size := 0
	for _, value := range text {
		if _, err := (mysqlstore.Text(value)).Value(); err != nil {
			return err
		}
		if len(value) > r.maxBytes-size {
			return ErrProjectionLimit
		}
		size += len(value)
	}
	return nil
}

func (r platformRepository) creation(tenant, id string, at time.Time, text ...string) (mysqlstore.UUID, error) {
	if err := r.scope(tenant, text...); err != nil {
		return mysqlstore.UUID{}, err
	}
	if !r.write {
		return mysqlstore.UUID{}, errors.New("platform creation requires a mutation transaction")
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil {
		return mysqlstore.UUID{}, ErrTenantScope
	}
	if _, err := (mysqlstore.DateTime{Time: at}).Value(); err != nil {
		return mysqlstore.UUID{}, err
	}
	return mysqlstore.UUID(parsed), nil
}

// Server-side projection suppresses oversized text, carrying its original byte
// size in the SAME SELECT. Never fetch a huge value and then truncate it in Go.
// Column names are private, fixed SQL identifiers, not request values.
func (r platformRepository) projection(columns ...string) string {
	lengths := make([]string, len(columns))
	for i, column := range columns {
		lengths[i] = "OCTET_LENGTH(" + column + ")"
	}
	size := strings.Join(lengths, "+")
	parts := []string{size}
	for _, column := range columns {
		parts = append(parts, fmt.Sprintf("CASE WHEN %s<=%d THEN %s ELSE '' END", size, r.maxBytes, column))
	}
	return strings.Join(parts, ",")
}

type rowScanner interface{ Scan(...any) error }

func platformList[T any](ctx context.Context, r platformRepository, query string, args []any, scan func(rowScanner) (T, int64, error)) ([]T, error) {
	rows, err := r.tx.QueryContext(ctx, query+" LIMIT ?", append(args, r.maxRows+1)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	var used int64
	for rows.Next() {
		if len(result) == r.maxRows {
			return nil, ErrProjectionLimit
		}
		value, size, err := scan(rows)
		if err != nil {
			return nil, err
		}
		if size < 0 || size > int64(r.maxBytes)-used {
			return nil, ErrProjectionLimit
		}
		used += size
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func platformOne[T any](r platformRepository, row *sql.Row, scan func(rowScanner) (T, int64, error)) (T, error) {
	value, size, err := scan(row)
	if err == nil && (size < 0 || size > int64(r.maxBytes)) {
		err = ErrProjectionLimit
	}
	if err != nil {
		var empty T
		return empty, err
	}
	return value, nil
}

// Hashes are NON-UNIQUE lookup accelerators. Full binary text predicates remain
// mandatory, including after a collision; no prefix or digest is an identity.
const objectWhere = "tenant_id=? AND object_type_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND object_id_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(object_type AS BINARY)=CAST(? AS BINARY) AND CAST(object_id AS BINARY)=CAST(? AS BINARY)"

func (r platformRepository) objectArgs(objectType, objectID string) []any {
	return []any{mysqlstore.UUID(r.tenant), mysqlstore.Text(objectType), mysqlstore.Text(objectID), mysqlstore.Text(objectType), mysqlstore.Text(objectID)}
}
