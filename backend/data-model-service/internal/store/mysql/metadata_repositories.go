package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// MetadataRepositories binds all operations to one authorized tenant and one
// caller-owned transaction. It does not commit, publish contracts, perform DDL,
// or authorize the caller. Owner services must provide those guarantees.
type MetadataRepositories struct {
	Tenants    TenantRepository
	Tables     TableRepository
	Fields     FieldRepository
	Enums      FieldEnumValueRepository
	Links      LinkRepository
	Navigation NavigationOptionRepository
	Pivots     PivotRepository
	History    SchemaChangeRepository
	Revisions  TenantSchemaMigrationRepository
	Options    TableOptionsRepository
	Jobs       IndexJobRepository
}

type metadataRepository struct {
	tx                *sql.Tx
	schema            string
	tenant            uuid.UUID
	maxRows, maxBytes int
}

func NewMetadataRepositories(tx *sql.Tx, database string, tenantID uuid.UUID, maxRows, maxBytes int) (MetadataRepositories, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return MetadataRepositories{}, err
	}
	if tx == nil || tenantID == uuid.Nil || maxRows < 1 || maxRows > 100000 || maxBytes < 1 || maxBytes > 64<<20 {
		return MetadataRepositories{}, errors.New("transaction, tenant and bounded row/byte limits required")
	}
	r := metadataRepository{tx, schema, tenantID, maxRows, maxBytes}
	return MetadataRepositories{Tenants: TenantRepository{r}, Tables: TableRepository{r}, Fields: FieldRepository{r}, Enums: FieldEnumValueRepository{r}, Links: LinkRepository{r}, Navigation: NavigationOptionRepository{r}, Pivots: PivotRepository{r}, History: SchemaChangeRepository{r}, Revisions: TenantSchemaMigrationRepository{r}, Options: TableOptionsRepository{r}, Jobs: IndexJobRepository{r}}, nil
}

func (r metadataRepository) validate(tenantID, id uuid.UUID, values ...string) error {
	if tenantID != r.tenant || id == uuid.Nil {
		return errors.New("invalid metadata identity or tenant scope")
	}
	size := 0
	for _, value := range values {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return errors.New("metadata text must be UTF-8 without NUL")
		}
		if len(value) > r.maxBytes-size {
			return ErrSnapshotLimit
		}
		size += len(value)
	}
	return nil
}

// lock checks existence even for unchanged UPDATEs, independently of the
// driver's affected-row mode. Only the exact scoped record is locked.
func (r metadataRepository) lock(ctx context.Context, table string, id uuid.UUID, parent string, parentID uuid.UUID) error {
	query := "SELECT id FROM " + r.schema + "." + table + " WHERE tenant_id=? AND id=?"
	args := []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)}
	if parent != "" {
		query += " AND " + parent + "=?"
		args = append(args, mysqlstore.UUID(parentID))
	}
	var found mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, query+" FOR UPDATE", args...).Scan(&found)
}

func (r metadataRepository) delete(ctx context.Context, table string, id uuid.UUID) error {
	result, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+"."+table+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// Projection suppresses oversized text on the server in the SAME statement as
// the read. The original size travels with the row, so no truncated model can
// be returned and a concurrent edit cannot bypass a separate size preflight.
func (r metadataRepository) projection(columns ...string) string {
	lengths := make([]string, len(columns))
	for i, col := range columns {
		lengths[i] = "OCTET_LENGTH(" + col + ")"
	}
	size := strings.Join(lengths, "+")
	projected := []string{size}
	for _, col := range columns {
		projected = append(projected, fmt.Sprintf("CASE WHEN %s<=%d THEN %s ELSE '' END", size, r.maxBytes, col))
	}
	return strings.Join(projected, ",")
}

type metadataScanner interface{ Scan(...any) error }

func metadataList[T any](ctx context.Context, r metadataRepository, query string, args []any, scan func(metadataScanner) (T, int64, error)) ([]T, error) {
	args = append(args, r.maxRows+1)
	rows, err := r.tx.QueryContext(ctx, query+" LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	var used int64
	for rows.Next() {
		if len(result) == r.maxRows {
			return nil, ErrSnapshotLimit
		}
		value, size, err := scan(rows)
		if err != nil {
			return nil, err
		}
		if size > int64(r.maxBytes)-used {
			return nil, ErrSnapshotLimit
		}
		used += size
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func metadataOne[T any](r metadataRepository, row *sql.Row, scan func(metadataScanner) (T, int64, error)) (T, error) {
	value, size, err := scan(row)
	if err == nil && size > int64(r.maxBytes) {
		err = ErrSnapshotLimit
	}
	if err != nil {
		var empty T
		return empty, err
	}
	return value, nil
}

var (
	_ ports.IndexJobRepository              = IndexJobRepository{}
	_ ports.TableOptionsRepository          = TableOptionsRepository{}
	_ ports.TenantRepository                = TenantRepository{}
	_ ports.TableRepository                 = TableRepository{}
	_ ports.FieldRepository                 = FieldRepository{}
	_ ports.FieldEnumValueRepository        = FieldEnumValueRepository{}
	_ ports.LinkRepository                  = LinkRepository{}
	_ ports.NavigationOptionRepository      = NavigationOptionRepository{}
	_ ports.PivotRepository                 = PivotRepository{}
	_ ports.SchemaChangeRepository          = SchemaChangeRepository{}
	_ ports.TenantSchemaMigrationRepository = TenantSchemaMigrationRepository{}
)
