// Package mysql implements staged MySQL owner adapters. It is not wired into
// the application until schema, permissions and DDL readiness gates are met.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrSnapshotLimit = errors.New("publication metadata exceeds configured snapshot limit")

type PublicationStore struct {
	db                  *sql.DB
	schema              string
	maxFields, maxEnums int
	maxBytes            int
}

// NewPublicationStore does not create schema or grant runtime privileges.
// Limits reject oversize snapshots explicitly; they never truncate publication.
func NewPublicationStore(db *sql.DB, database string, maxFields, maxEnums, maxBytes int) (*PublicationStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || maxFields <= 0 || maxEnums <= 0 || maxFields > 1000000 || maxEnums > 1000000 {
		return nil, errors.New("database and snapshot limits in 1..1000000 required")
	}
	if maxBytes <= 0 || maxBytes > 64<<20 {
		return nil, errors.New("publication byte limit in 1..67108864 required")
	}
	return &PublicationStore{db: db, schema: schema, maxFields: maxFields, maxEnums: maxEnums, maxBytes: maxBytes}, nil
}

// MutateAndPublish serializes edits to ONE existing table, including first
// publication. Every metadata writer must use the same table lock before edits.
// mutate is an internal owner-adapter callback: no DDL, commit/rollback, session
// changes, unrelated-table mutations or network calls. Authorization and the
// durable operation/readiness checks belong to its caller. A return value is
// available only after acknowledged commit; CommitError must be reconciled by
// operation identity, never retried blindly. This is not a public service API.
func (s *PublicationStore) MutateAndPublish(ctx context.Context, tenantID, tableID uuid.UUID, mutate func(*sql.Tx) error) (contract.Publication, error) {
	return s.mutateAndPublish(ctx, tenantID, tableID, nil, mutate)
}

func (s *PublicationStore) mutateAndPublish(ctx context.Context, tenantID, tableID uuid.UUID, operation *PublicationOperation, mutate func(*sql.Tx) error) (contract.Publication, error) {
	if tenantID == uuid.Nil || tableID == uuid.Nil || mutate == nil {
		return contract.Publication{}, errors.New("tenant, table and mutation required")
	}
	var result contract.Publication
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		result, err = s.mutateAndPublishTx(ctx, tx, tenantID, tableID, operation, mutate)
		return err
	})
	if err != nil {
		return contract.Publication{}, err
	}
	return result, nil
}

func (s *PublicationStore) mutateAndPublishTx(ctx context.Context, tx *sql.Tx, tenantID, tableID uuid.UUID, operation *PublicationOperation, mutate func(*sql.Tx) error) (contract.Publication, error) {
	var locked mysqlstore.UUID
	if err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&locked); err != nil {
		return contract.Publication{}, fmt.Errorf("lock publication table: %w", err)
	}
	if operation != nil {
		receipt, err := s.readOperation(ctx, tx, tenantID, tableID, *operation)
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return contract.Publication{}, err
		}
	}
	previous := contract.Publication{TenantID: tenantID, TableID: tableID}
	var token mysqlstore.UUID
	var payload []byte
	err := tx.QueryRowContext(ctx, "SELECT version,CASE WHEN OCTET_LENGTH(payload)<=? THEN payload ELSE NULL END FROM "+s.schema+".data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", s.maxBytes, mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&token, &payload)
	var prior *contract.Publication
	if err == nil {
		if payload == nil {
			return contract.Publication{}, ErrSnapshotLimit
		}
		previous.Payload = payload
		previous.Version = uuid.UUID(token)
		prior = &previous
	} else if !errors.Is(err, sql.ErrNoRows) {
		return contract.Publication{}, fmt.Errorf("read previous publication: %w", err)
	}
	if operation != nil {
		if err := operation.Condition.Check(prior); err != nil {
			return contract.Publication{}, err
		}
	}
	if err := mutate(tx); err != nil {
		return contract.Publication{}, err
	}
	table, fields, enums, err := s.snapshot(ctx, tx, tenantID, tableID)
	if err != nil {
		return contract.Publication{}, err
	}
	next, changed, err := contract.PrepareIngestion(table, fields, enums, prior, uuid.NewRandom)
	if err != nil {
		return contract.Publication{}, err
	}
	if len(next.Payload) > s.maxBytes {
		return contract.Publication{}, ErrSnapshotLimit
	}
	if changed {
		// MySQL's textual JSON representation can be larger than Go's compact
		// encoding. Reject it now so every committed result remains readable
		// under the same configured limit, including receipt recovery.
		var storedBytes int
		if err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(CAST(? AS JSON))", string(next.Payload)).Scan(&storedBytes); err != nil {
			return contract.Publication{}, fmt.Errorf("measure publication JSON: %w", err)
		}
		if storedBytes > s.maxBytes {
			return contract.Publication{}, ErrSnapshotLimit
		}
		if prior == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_ingestion_contracts (tenant_id,table_id,table_name,version,payload) VALUES (?,?,?,?,?)", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), table.Name, mysqlstore.UUID(next.Version), string(next.Payload))
		} else {
			var updated sql.Result
			updated, err = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_ingestion_contracts SET table_name=?,version=?,payload=? WHERE tenant_id=? AND table_id=? AND version=?", table.Name, mysqlstore.UUID(next.Version), string(next.Payload), mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(prior.Version))
			if err == nil {
				var count int64
				count, err = updated.RowsAffected()
				if err == nil && count != 1 {
					err = errors.New("publication version changed outside owner lock")
				}
			}
		}
		if err != nil {
			return contract.Publication{}, fmt.Errorf("persist ingestion publication: %w", err)
		}
	}
	if operation != nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_publication_operations (tenant_id,table_id,operation_id,request_hash,condition_mode,expected_version,version,payload) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(operation.ID), operation.RequestHash[:], operation.Condition.Mode, mysqlstore.UUID(operation.Condition.Version), mysqlstore.UUID(next.Version), string(next.Payload))
		if err != nil {
			return contract.Publication{}, fmt.Errorf("persist publication receipt: %w", err)
		}
	}
	return next, nil
}

func (s *PublicationStore) snapshot(ctx context.Context, tx *sql.Tx, tenantID, tableID uuid.UUID) (datamodel.Table, []datamodel.Field, []datamodel.FieldEnumValue, error) {
	table := datamodel.Table{ID: tableID, TenantID: tenantID}
	var name sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT CASE WHEN OCTET_LENGTH(name)<=? THEN name ELSE NULL END,archived FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=?", s.maxBytes, mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&name, &table.Archived); err != nil {
		return table, nil, nil, err
	}
	if !name.Valid {
		return table, nil, nil, ErrSnapshotLimit
	}
	table.Name = name.String
	remaining := s.maxBytes - len(table.Name)
	fields, err := s.fields(ctx, tx, tenantID, tableID, &remaining)
	if err != nil {
		return table, nil, nil, err
	}
	enums, err := s.enums(ctx, tx, tenantID, tableID, &remaining)
	return table, fields, enums, err
}

func (s *PublicationStore) fields(ctx context.Context, tx *sql.Tx, tenantID, tableID uuid.UUID, remaining *int) ([]datamodel.Field, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,CASE WHEN OCTET_LENGTH(name)+OCTET_LENGTH(data_type)<=? THEN name ELSE NULL END,CASE WHEN OCTET_LENGTH(name)+OCTET_LENGTH(data_type)<=? THEN data_type ELSE NULL END,nullable,is_enum,is_unique,archived FROM "+s.schema+".data_model_fields WHERE tenant_id=? AND table_id=? ORDER BY id LIMIT ?", *remaining, *remaining, mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), s.maxFields+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []datamodel.Field
	for rows.Next() {
		if len(fields) == s.maxFields {
			return nil, ErrSnapshotLimit
		}
		f := datamodel.Field{TenantID: tenantID, TableID: tableID}
		var id mysqlstore.UUID
		var name, dataType sql.NullString
		if err := rows.Scan(&id, &name, &dataType, &f.Nullable, &f.IsEnum, &f.IsUnique, &f.Archived); err != nil {
			return nil, err
		}
		if !name.Valid || !dataType.Valid || len(name.String)+len(dataType.String) > *remaining {
			return nil, ErrSnapshotLimit
		}
		*remaining -= len(name.String) + len(dataType.String)
		f.Name, f.DataType = name.String, datamodel.DataType(dataType.String)
		f.ID = uuid.UUID(id)
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

func (s *PublicationStore) enums(ctx context.Context, tx *sql.Tx, tenantID, tableID uuid.UUID, remaining *int) ([]datamodel.FieldEnumValue, error) {
	rows, err := tx.QueryContext(ctx, "SELECT e.id,e.field_id,CASE WHEN OCTET_LENGTH(e.value)+OCTET_LENGTH(e.label)<=? THEN e.value ELSE NULL END,CASE WHEN OCTET_LENGTH(e.value)+OCTET_LENGTH(e.label)<=? THEN e.label ELSE NULL END,e.sort_order FROM "+s.schema+".data_model_field_enum_values e JOIN "+s.schema+".data_model_fields f ON f.tenant_id=e.tenant_id AND f.id=e.field_id WHERE f.tenant_id=? AND f.table_id=? ORDER BY e.id LIMIT ?", *remaining, *remaining, mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), s.maxEnums+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []datamodel.FieldEnumValue
	for rows.Next() {
		if len(values) == s.maxEnums {
			return nil, ErrSnapshotLimit
		}
		v := datamodel.FieldEnumValue{TenantID: tenantID}
		var id, fieldID mysqlstore.UUID
		var value, label sql.NullString
		if err := rows.Scan(&id, &fieldID, &value, &label, &v.SortOrder); err != nil {
			return nil, err
		}
		if !value.Valid || !label.Valid || len(value.String)+len(label.String) > *remaining {
			return nil, ErrSnapshotLimit
		}
		*remaining -= len(value.String) + len(label.String)
		v.Value, v.Label = value.String, label.String
		v.ID, v.FieldID = uuid.UUID(id), uuid.UUID(fieldID)
		values = append(values, v)
	}
	return values, rows.Err()
}
