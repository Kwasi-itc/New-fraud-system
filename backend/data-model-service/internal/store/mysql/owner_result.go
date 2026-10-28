package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrOwnerResultUnavailable = errors.New("original owner result unavailable; current state cannot replace it")

type ownerResultReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type ownerResults struct {
	schema   string
	tenant   uuid.UUID
	maxBytes int
}

func (s ownerResults) read(ctx context.Context, q ownerResultReader, table, operation uuid.UUID, expectedKind string) ([]byte, error) {
	var kind string
	var raw []byte
	err := q.QueryRowContext(ctx, "SELECT result_kind,CASE WHEN OCTET_LENGTH(CAST(original_result AS CHAR CHARACTER SET utf8mb4))<=? THEN original_result ELSE NULL END FROM "+s.schema+".data_model_owner_results WHERE tenant_id=? AND table_id=? AND operation_id=?", s.maxBytes, mysqlstore.UUID(s.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOwnerResultUnavailable
	}
	if err != nil {
		return nil, err
	}
	if kind != expectedKind {
		return nil, ErrPublicationOperationConflict
	}
	if raw == nil {
		return nil, ErrSnapshotLimit
	}
	return raw, nil
}
func (s ownerResults) write(ctx context.Context, tx *sql.Tx, table, operation uuid.UUID, kind string, raw []byte) error {
	if len(raw) > s.maxBytes {
		return ErrSnapshotLimit
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_owner_results(tenant_id,table_id,operation_id,result_kind,original_result,created_at) VALUES (?,?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation), kind, mysqlstore.JSON(raw)); err != nil {
		return err
	}
	var size int
	if err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(CAST(original_result AS CHAR CHARACTER SET utf8mb4)) FROM "+s.schema+".data_model_owner_results WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&size); err != nil {
		return err
	}
	if size > s.maxBytes {
		return ErrSnapshotLimit
	}
	return nil
}
