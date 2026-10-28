package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type CreationReceiptIdentity struct {
	OperationID, ActorID uuid.UUID
	RequestHash          [32]byte
}

type CreationReceipt struct {
	Snapshot        ports.TableCreationSnapshot
	PublicationHash [32]byte
	Replayed        bool
}

// CreationReceipts is transaction-bound and tenant-bound. The owner must
// authorize every operation before use, including replay; rollback every error.
// Reserve must precede new model/registry/intent locks. It never performs DDL,
// commits, or reports preparation as completed physical creation.
type CreationReceipts struct {
	tx     *sql.Tx
	schema string
	tenant uuid.UUID
}

func NewCreationReceipts(tx *sql.Tx, database string, tenant uuid.UUID) (*CreationReceipts, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if tx == nil || tenant == uuid.Nil {
		return nil, errors.New("owner transaction and tenant required")
	}
	return &CreationReceipts{tx, schema, tenant}, nil
}

func (id CreationReceiptIdentity) validate() error {
	if id.OperationID == uuid.Nil || id.ActorID == uuid.Nil || id.RequestHash == ([32]byte{}) {
		return errors.New("creation operation, trusted actor and request hash required")
	}
	return nil
}

func validateCreationSnapshot(v ports.TableCreationSnapshot, tenant uuid.UUID) error {
	t := v.Table
	if v.Format != "dm-table-creation-result/v1" || t.ID == uuid.Nil || t.TenantID != tenant || t.Archived || t.CaptionField != "" || t.CreatedAt.IsZero() || !t.UpdatedAt.Equal(t.CreatedAt) || t.CreatedAt.Year() < 1000 || t.CreatedAt.Year() > 9999 || t.CreatedAt.Nanosecond()%1000 != 0 {
		return errors.New("invalid original creation snapshot")
	}
	if err := datamodel.ValidateTableCreate(t.Name); err != nil {
		return err
	}
	if err := datamodel.ValidateSemanticType(t.SemanticType); err != nil {
		return err
	}
	if datamodel.NormalizeName(t.Name) != t.Name || datamodel.NormalizeName(t.SemanticType) != t.SemanticType {
		return errors.New("original table must contain normalized owner metadata")
	}
	texts := []string{t.Name, t.Description, t.Alias, t.SemanticType}
	for i, f := range v.Fields {
		if f.ID == uuid.Nil || f.ID == t.ID || f.TenantID != tenant || f.TableID != t.ID || f.Archived || f.Nullable || f.IsEnum || !f.CreatedAt.Equal(t.CreatedAt) || !f.UpdatedAt.Equal(t.CreatedAt) {
			return errors.New("invalid original mandatory field")
		}
		if i == 0 && (f.Name != "object_id" || f.DataType != datamodel.DataTypeString || !f.IsUnique) {
			return errors.New("invalid original object_id")
		}
		if i == 1 && (f.Name != "updated_at" || f.DataType != datamodel.DataTypeTimestamp || f.IsUnique || f.ID == v.Fields[0].ID) {
			return errors.New("invalid original updated_at")
		}
		texts = append(texts, f.Name, f.Description, f.Alias)
	}
	total := 0
	for _, s := range texts {
		if !utf8.ValidString(s) {
			return errors.New("invalid UTF-8 in original creation")
		}
		total += len(s)
		if total > 1<<20 {
			return ErrSnapshotLimit
		}
	}
	return nil
}

func (r *CreationReceipts) Reserve(ctx context.Context, id CreationReceiptIdentity, candidate ports.TableCreationSnapshot, publicationHash [32]byte) (CreationReceipt, error) {
	if err := id.validate(); err != nil {
		return CreationReceipt{}, err
	}
	if err := validateCreationSnapshot(candidate, r.tenant); err != nil {
		return CreationReceipt{}, err
	}
	if publicationHash == ([32]byte{}) {
		return CreationReceipt{}, errors.New("allocated-table publication hash required")
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return CreationReceipt{}, err
	}
	if len(raw) > 1<<20 {
		return CreationReceipt{}, ErrSnapshotLimit
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_table_creation_operations(tenant_id,operation_id,table_id,actor_id,request_hash,publication_hash,original_snapshot,created_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id.OperationID), mysqlstore.UUID(candidate.Table.ID), mysqlstore.UUID(id.ActorID), id.RequestHash[:], publicationHash[:], mysqlstore.JSON(raw), candidate.Table.CreatedAt)
	replay := false
	if err != nil {
		var duplicate *driver.MySQLError
		if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
			return CreationReceipt{}, err
		}
		// Only a matching retained operation can explain a duplicate. In particular,
		// a table allocated to another operation is a conflict, never adopted.
		replay = true
	}
	result, err := r.Read(ctx, id)
	if errors.Is(err, sql.ErrNoRows) && replay {
		return CreationReceipt{}, ErrPublicationOperationConflict
	}
	if err != nil {
		return CreationReceipt{}, err
	}
	result.Replayed = replay
	return result, nil
}

// Read returns immutable allocation evidence, not a successful create result.
// Exact-key lookup is bounded before transferring the JSON document.
func (r *CreationReceipts) Read(ctx context.Context, id CreationReceiptIdentity) (CreationReceipt, error) {
	if err := id.validate(); err != nil {
		return CreationReceipt{}, err
	}
	var matches bool
	var table mysqlstore.UUID
	var hash []byte
	var raw sql.NullString
	err := r.tx.QueryRowContext(ctx, "SELECT actor_id=? AND request_hash=?,table_id,publication_hash,CASE WHEN OCTET_LENGTH(CAST(original_snapshot AS CHAR CHARACTER SET utf8mb4))<=1048576 THEN CAST(original_snapshot AS CHAR CHARACTER SET utf8mb4) END FROM "+r.schema+".data_model_table_creation_operations WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(id.ActorID), id.RequestHash[:], mysqlstore.UUID(r.tenant), mysqlstore.UUID(id.OperationID)).Scan(&matches, &table, &hash, &raw)
	if err != nil {
		return CreationReceipt{}, err
	}
	if !matches {
		return CreationReceipt{}, ErrPublicationOperationConflict
	}
	if !raw.Valid {
		return CreationReceipt{}, ErrSnapshotLimit
	}
	var result CreationReceipt
	decoder := json.NewDecoder(bytes.NewBufferString(raw.String))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.Snapshot); err != nil {
		return CreationReceipt{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return CreationReceipt{}, errors.New("trailing creation snapshot")
	}
	if len(hash) != 32 || result.Snapshot.Table.ID != uuid.UUID(table) {
		return CreationReceipt{}, ErrPhysicalConflict
	}
	copy(result.PublicationHash[:], hash)
	if result.PublicationHash == ([32]byte{}) {
		return CreationReceipt{}, ErrPhysicalConflict
	}
	if err := validateCreationSnapshot(result.Snapshot, r.tenant); err != nil {
		return CreationReceipt{}, err
	}
	result.Replayed = true
	return result, nil
}

// PublishedTable returns the ORIGINAL owner result only when both the immutable
// publication receipt and published intent agree. Historical replay does not
// reopen a newer barrier or attest to current record availability.
func (r *CreationReceipts) PublishedTable(ctx context.Context, id CreationReceiptIdentity) (datamodel.Table, error) {
	receipt, err := r.Read(ctx, id)
	if err != nil {
		return datamodel.Table{}, err
	}
	var ready bool
	err = r.tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.schema+".data_model_schema_change_intents i JOIN "+r.schema+".data_model_publication_operations p ON p.tenant_id=i.tenant_id AND p.table_id=i.table_id AND p.operation_id=i.operation_id WHERE i.tenant_id=? AND i.table_id=? AND i.operation_id=? AND i.state='published' AND i.actor_kind='api' AND BINARY i.actor_id=BINARY ? AND i.request_hash=? AND i.expected_version IS NULL AND p.request_hash=i.request_hash AND p.condition_mode=? AND p.expected_version=?)", mysqlstore.UUID(r.tenant), mysqlstore.UUID(receipt.Snapshot.Table.ID), mysqlstore.UUID(id.OperationID), id.ActorID.String(), receipt.PublicationHash[:], PublicationMustBeAbsent, mysqlstore.UUID(uuid.Nil)).Scan(&ready)
	if err != nil {
		return datamodel.Table{}, err
	}
	if !ready {
		return datamodel.Table{}, ErrModelUnavailable
	}
	return receipt.Snapshot.Table, nil
}
