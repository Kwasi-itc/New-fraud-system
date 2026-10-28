package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// DDLIntent is a trusted owner request, not an authorized HTTP DTO. The owner
// must canonicalize/bind the whole command and reauthorize every replay. Shape
// is a versioned owner plan, never SQL to be accepted directly from API callers.
type DDLIntent struct {
	TableID, OperationID uuid.UUID
	RequestHash          [32]byte
	ActorKind, ActorID   string
	ExpectedEpoch        int64
	ExpectedVersion      *uuid.UUID
	Shape                json.RawMessage
}

type DDLIntentResult struct {
	OperationID uuid.UUID
	State       string
	Replayed    bool
}

type DDLIntentStore struct {
	db       *sql.DB
	schema   string
	tenant   uuid.UUID
	maxBytes int
}

func NewDDLIntentStore(db *sql.DB, database string, tenant uuid.UUID, maxBytes int) (*DDLIntentStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || maxBytes < 1 || maxBytes > 1<<20 {
		return nil, errors.New("database, tenant and plan limit 1..1 MiB required")
	}
	return &DDLIntentStore{db, schema, tenant, maxBytes}, nil
}

func (s *DDLIntentStore) validate(v DDLIntent) error {
	if v.TableID == uuid.Nil || v.OperationID == uuid.Nil || v.RequestHash == [32]byte{} || v.ExpectedEpoch < 0 || v.ExpectedEpoch == math.MaxInt64 || (v.ExpectedVersion != nil && *v.ExpectedVersion == uuid.Nil) {
		return errors.New("nonzero intent identities/hash and bounded epoch/version required")
	}
	switch v.ActorKind {
	case "api", "worker", "reconcile", "migrate":
	default:
		return errors.New("trusted actor kind required")
	}
	if v.ActorID == "" || !utf8.ValidString(v.ActorID) || strings.ContainsRune(v.ActorID, 0) || utf8.RuneCountInString(v.ActorID) > 512 {
		return errors.New("bounded UTF-8 trusted actor identity required")
	}
	if len(v.Shape) > s.maxBytes {
		return ErrSnapshotLimit
	}
	shape := strings.TrimSpace(string(v.Shape))
	if !utf8.Valid(v.Shape) || !json.Valid(v.Shape) || len(shape) == 0 || shape[0] != '{' {
		return errors.New("desired shape must be a UTF-8 JSON object")
	}
	return nil
}

// Prepare owns a short transaction and locks only the exact table registry.
// Previous physical writers must finish before the barrier commits. It does no
// DDL and returns an empty result on any failure, including ambiguous commit.
// A matching historical replay does not change the current registry/barrier.
func (s *DDLIntentStore) Prepare(ctx context.Context, v DDLIntent) (DDLIntentResult, error) {
	if err := s.validate(v); err != nil {
		return DDLIntentResult{}, err
	}
	var result DDLIntentResult
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		return s.prepareTx(ctx, tx, v, &result)
	})
	if err != nil {
		return DDLIntentResult{}, err
	}
	return result, nil
}

// PrepareTx joins the owner's metadata/mapping transaction. The owner must
// authorize first and use the same database, lock model rows before registry,
// and roll back the entire transaction on any error. Results are provisional
// until the outer commit; this method never commits or executes DDL.
func (s *DDLIntentStore) PrepareTx(ctx context.Context, tx *sql.Tx, v DDLIntent) (DDLIntentResult, error) {
	if s == nil || tx == nil {
		return DDLIntentResult{}, errors.New("intent store and owner transaction required")
	}
	if err := s.validate(v); err != nil {
		return DDLIntentResult{}, err
	}
	var result DDLIntentResult
	if err := s.prepareTx(ctx, tx, v, &result); err != nil {
		return DDLIntentResult{}, err
	}
	return result, nil
}

func (s *DDLIntentStore) prepareTx(ctx context.Context, tx *sql.Tx, v DDLIntent, result *DDLIntentResult) error {
	var epoch int64
	var state string
	var version, active mysqlstore.NullUUID
	if err := tx.QueryRowContext(ctx, "SELECT epoch,write_state,published_version,active_operation FROM "+s.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID)).Scan(&epoch, &state, &version, &active); err != nil {
		return err
	}
	var matches bool
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT table_id=? AND request_hash=? AND actor_kind=? AND BINARY actor_id=BINARY ? AND expected_epoch=? AND (expected_version<=>?) AND desired_shape=CAST(? AS JSON),state FROM "+s.schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=? FOR UPDATE", mysqlstore.UUID(v.TableID), v.RequestHash[:], v.ActorKind, v.ActorID, v.ExpectedEpoch, nullableUUID(v.ExpectedVersion), mysqlstore.JSON(v.Shape), mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.OperationID)).Scan(&matches, &previous)
	if err == nil {
		if !matches {
			return ErrPhysicalConflict
		}
		*result = DDLIntentResult{v.OperationID, previous, true}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if state == "retired" || active.Valid || epoch != v.ExpectedEpoch || version.Valid != (v.ExpectedVersion != nil) || (version.Valid && uuid.UUID(version.UUID) != *v.ExpectedVersion) {
		return ErrPhysicalConflict
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_schema_change_intents(tenant_id,table_id,operation_id,request_hash,actor_kind,actor_id,expected_epoch,expected_version,desired_shape,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID), mysqlstore.UUID(v.OperationID), v.RequestHash[:], v.ActorKind, v.ActorID, v.ExpectedEpoch, nullableUUID(v.ExpectedVersion), mysqlstore.JSON(v.Shape))
	if err != nil {
		return err
	}
	var storedBytes int
	if err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(CAST(desired_shape AS CHAR CHARACTER SET utf8mb4)) FROM "+s.schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.OperationID)).Scan(&storedBytes); err != nil {
		return err
	}
	if storedBytes > s.maxBytes {
		return ErrSnapshotLimit
	}
	_, err = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_physical_tables SET write_state='closed',active_operation=?,epoch=epoch+1 WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(v.OperationID), mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID))
	if err != nil {
		return err
	}
	*result = DDLIntentResult{v.OperationID, "prepared", false}
	return nil
}

// Pending is an indexed tenant/state/UUID keyset page. It projects only operation
// identities, not arbitrary histories or plans. Recovery must inspect each plan
// under the table's named DDL lock; queue delivery is not the source of truth.
func (s *DDLIntentStore) Pending(ctx context.Context, state string, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	switch state {
	case "prepared", "applying", "verifying", "blocked":
	default:
		return nil, errors.New("pending intent state required")
	}
	if limit < 1 || limit > 1000 {
		return nil, errors.New("page limit must be 1..1000")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT operation_id FROM "+s.schema+".data_model_schema_change_intents WHERE tenant_id=? AND state=? AND operation_id>? ORDER BY operation_id LIMIT ?", mysqlstore.UUID(s.tenant), state, mysqlstore.UUID(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]uuid.UUID, 0)
	for rows.Next() {
		var id mysqlstore.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, uuid.UUID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// Read bounds plan text on the server before transferring it. This is internal
// recovery input, not proof of authorization, shape verification or publication.
func (s *DDLIntentStore) Read(ctx context.Context, operation uuid.UUID) (DDLIntent, string, error) {
	if operation == uuid.Nil {
		return DDLIntent{}, "", errors.New("operation required")
	}
	var v DDLIntent
	var hash []byte
	var version mysqlstore.NullUUID
	var shape sql.NullString
	var size int
	var state string
	err := s.db.QueryRowContext(ctx, "SELECT table_id,operation_id,request_hash,actor_kind,actor_id,expected_epoch,expected_version,CASE WHEN OCTET_LENGTH(CAST(desired_shape AS CHAR CHARACTER SET utf8mb4))<=? THEN CAST(desired_shape AS CHAR CHARACTER SET utf8mb4) ELSE NULL END,OCTET_LENGTH(CAST(desired_shape AS CHAR CHARACTER SET utf8mb4)),state FROM "+s.schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=?", s.maxBytes, mysqlstore.UUID(s.tenant), mysqlstore.UUID(operation)).Scan((*mysqlstore.UUID)(&v.TableID), (*mysqlstore.UUID)(&v.OperationID), &hash, &v.ActorKind, &v.ActorID, &v.ExpectedEpoch, &version, &shape, &size, &state)
	if err != nil {
		return DDLIntent{}, "", err
	}
	if !shape.Valid || size > s.maxBytes {
		return DDLIntent{}, "", ErrSnapshotLimit
	}
	if len(hash) != 32 {
		return DDLIntent{}, "", errors.New("invalid intent hash")
	}
	copy(v.RequestHash[:], hash)
	v.Shape = json.RawMessage(shape.String)
	if version.Valid {
		id := uuid.UUID(version.UUID)
		v.ExpectedVersion = &id
	}
	return v, state, nil
}

func nullableUUID(v *uuid.UUID) any {
	if v == nil {
		return nil
	}
	return mysqlstore.UUID(*v)
}
