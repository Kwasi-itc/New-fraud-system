package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrPublicationOperationConflict = errors.New("publication operation reused with a different request")
var ErrPublicationVersionConflict = contract.ErrPublicationVersionConflict

type PublicationConditionMode = contract.PublicationConditionMode
type PublicationCondition = contract.PublicationCondition

const (
	PublicationAnyVersion   = contract.PublicationAnyVersion
	PublicationMustBeAbsent = contract.PublicationMustBeAbsent
	PublicationExactVersion = contract.PublicationExactVersion
)

// PublicationOperation is scoped by tenant and table. The owner must compute
// SHA-256 over a versioned canonical request including the operation kind,
// expected version, all mutation inputs and trusted actor identity. Authenticate
// and authorize every invocation, including recovery/replay. Never hash only
// the desired ingestion payload: presentation mutations may leave it unchanged.
type PublicationOperation struct {
	ID          uuid.UUID
	RequestHash [32]byte
	Condition   PublicationCondition
}

func (o PublicationOperation) validate() error {
	if o.ID == uuid.Nil || o.RequestHash == ([32]byte{}) {
		return errors.New("operation identity and request fingerprint required")
	}
	return o.Condition.Validate()
}

// MutateAndPublishOperation atomically records a replayable result. It never
// automatically retries. After an uncertain commit, call RecoverPublication on
// a healthy primary connection, or explicitly invoke this method with the SAME
// operation/request after authorization. A matching receipt bypasses mutate.
// Replay returns that operation's result, even if later edits changed the model.
func (s *PublicationStore) MutateAndPublishOperation(ctx context.Context, tenantID, tableID uuid.UUID, operation PublicationOperation, mutate func(*sql.Tx) error) (contract.Publication, error) {
	if err := operation.validate(); err != nil {
		return contract.Publication{}, err
	}
	return s.mutateAndPublish(ctx, tenantID, tableID, &operation, mutate)
}

// MutateAndPublishOperationTx composes publication with an existing owner/display
// transaction. The caller must use READ COMMITTED, authorize every invocation,
// establish physical readiness and lock all affected model tables in UUID order
// before combining multiple tables. This method locks its exact table as well.
// It does not commit: the result is provisional until the caller commits, and
// EVERY error requires rollback of the entire transaction. Replay skips mutate,
// not effects the caller performed before invoking this method. Put replayable
// effects inside mutate, or recover the receipt before repeating outer effects.
func (s *PublicationStore) MutateAndPublishOperationTx(ctx context.Context, tx *sql.Tx, tenantID, tableID uuid.UUID, operation PublicationOperation, mutate func(*sql.Tx) error) (contract.Publication, error) {
	if tx == nil || tenantID == uuid.Nil || tableID == uuid.Nil || mutate == nil {
		return contract.Publication{}, errors.New("transaction, tenant, table and mutation required")
	}
	if err := operation.validate(); err != nil {
		return contract.Publication{}, err
	}
	return s.mutateAndPublishTx(ctx, tx, tenantID, tableID, &operation, mutate)
}

// RecoverPublication waits on the same table guard as the original operation
// before reading its receipt. It never invokes a mutation. sql.ErrNoRows means
// no receipt is visible after acquiring that guard (or the table is absent),
// not permission to replay under a new operation ID. Errors/cancellation are
// not evidence that the original operation rolled back. Receipts must remain
// available for the full retry window; table retirement is a separate protocol.
func (s *PublicationStore) RecoverPublication(ctx context.Context, tenantID, tableID uuid.UUID, operation PublicationOperation) (contract.Publication, error) {
	if err := operation.validate(); err != nil {
		return contract.Publication{}, err
	}
	if tenantID == uuid.Nil || tableID == uuid.Nil {
		return contract.Publication{}, errors.New("tenant and table required")
	}
	var result contract.Publication
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		var locked mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&locked); err != nil {
			return fmt.Errorf("lock recovery table: %w", err)
		}
		var err error
		result, err = s.readOperation(ctx, tx, tenantID, tableID, operation)
		return err
	})
	if err != nil {
		return contract.Publication{}, err
	}
	return result, nil
}

func (s *PublicationStore) readOperation(ctx context.Context, tx *sql.Tx, tenantID, tableID uuid.UUID, operation PublicationOperation) (contract.Publication, error) {
	result := contract.Publication{TenantID: tenantID, TableID: tableID}
	var fingerprint []byte
	var version mysqlstore.UUID
	var conditionMode PublicationConditionMode
	var expectedVersion mysqlstore.UUID
	var payload []byte
	if err := tx.QueryRowContext(ctx, "SELECT request_hash,condition_mode,expected_version,version,CASE WHEN OCTET_LENGTH(payload)<=? THEN payload ELSE NULL END FROM "+s.schema+".data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", s.maxBytes, mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(operation.ID)).Scan(&fingerprint, &conditionMode, &expectedVersion, &version, &payload); err != nil {
		return contract.Publication{}, fmt.Errorf("read publication receipt: %w", err)
	}
	if !bytes.Equal(fingerprint, operation.RequestHash[:]) || conditionMode != operation.Condition.Mode || uuid.UUID(expectedVersion) != operation.Condition.Version {
		return contract.Publication{}, ErrPublicationOperationConflict
	}
	if payload == nil {
		return contract.Publication{}, ErrSnapshotLimit
	}
	result.Payload = payload
	result.Version = uuid.UUID(version)
	if result.Version == uuid.Nil {
		return contract.Publication{}, errors.New("publication receipt has invalid version")
	}
	return result, nil
}
