package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PhysicalPublicationRequest comes from an authorized owner command, never raw
// HTTP input. Its identity must match the durable DDL intent exactly. The hash
// binds the canonical owner mutation, scope, actor and publication condition.
type PhysicalPublicationRequest struct {
	TableID            uuid.UUID
	Operation          PublicationOperation
	ActorKind, ActorID string
}

func (r PhysicalPublicationRequest) validate() error {
	if r.TableID == uuid.Nil || r.ActorID == "" || !utf8.ValidString(r.ActorID) || strings.ContainsRune(r.ActorID, 0) || utf8.RuneCountInString(r.ActorID) > 512 {
		return errors.New("table and bounded trusted actor identity required")
	}
	switch r.ActorKind {
	case "api", "worker", "reconcile", "migrate":
	default:
		return errors.New("trusted actor kind required")
	}
	if err := r.Operation.validate(); err != nil {
		return err
	}
	if r.Operation.Condition.Mode == PublicationAnyVersion {
		return errors.New("physical publication requires absent or exact version condition")
	}
	return nil
}

// PublishVerifiedOperation holds the DDL lock from verification through commit.
// authorize must authenticate the actual caller, bind ActorKind/ActorID and check
// current tenant/table permission; it runs BEFORE database locks on every call,
// including historical replay. It may perform network authorization there.
// mutate is trusted, short, single-table owner SQL on the supplied transaction:
// business checks, metadata, audit, revisions and queue effects. It must ensure
// the owner-planned physical shape implements the desired metadata, and must not
// mutate registry/intent state, commit, execute DDL or call external services.
// Multi-table display mutations require their own ordered owner coordinator.
// Neither this adapter nor successful fixture verification activates runtime.
func (e *PhysicalExecutor) PublishVerifiedOperation(ctx context.Context, publication *PublicationStore, r PhysicalPublicationRequest, policies []PhysicalAccessPolicy,
	authorize func(context.Context, uuid.UUID, PhysicalPublicationRequest) error, mutate func(*sql.Tx) error,
) (contract.Publication, error) {
	return e.publishVerifiedOperation(ctx, publication, r, policies, authorize, mutate, nil)
}

// resultReceipt runs only after the publication receipt exists, on the same
// transaction and pinned execution connection. It must not commit or perform
// network/DDL work. Replay is read-only and must not recreate missing results.
func (e *PhysicalExecutor) publishVerifiedOperation(ctx context.Context, publication *PublicationStore, r PhysicalPublicationRequest, policies []PhysicalAccessPolicy, authorize func(context.Context, uuid.UUID, PhysicalPublicationRequest) error, mutate func(*sql.Tx) error, resultReceipt func(*sql.Tx, bool) error) (contract.Publication, error) {
	if err := r.validate(); err != nil {
		return contract.Publication{}, err
	}
	if publication == nil || publication.schema != e.schema || authorize == nil || mutate == nil {
		return contract.Publication{}, errors.New("same-database publication, authorization and owner mutation required")
	}
	if err := authorize(ctx, e.tenant, r); err != nil {
		return contract.Publication{}, err
	}
	sum := sha256.Sum256([]byte(e.database + ":" + e.tenant.String() + ":" + r.TableID.String()))
	var result contract.Publication
	err := mysqlstore.WithDDLLock(ctx, e.db, fmt.Sprintf("data_model_ddl_%x", sum[:24]), func(conn *sql.Conn) error {
		state, err := e.physicalPublicationIdentity(ctx, conn, r, false)
		if err != nil {
			return err
		}
		if state == "published" {
			// Historical replay reports the ORIGINAL result. It does not reopen a
			// newer barrier, rerun effects, or attest to current schema/grants.
			return physicalPublicationTx(ctx, conn, func(tx *sql.Tx) error {
				var err error
				result, err = publication.readOperation(ctx, tx, e.tenant, r.TableID, r.Operation)
				if err == nil && resultReceipt != nil {
					return resultReceipt(tx, true)
				}
				return err
			})
		}
		return e.withVerifiedPhysicalConn(ctx, conn, r.TableID, r.Operation.ID, policies, func(conn *sql.Conn) error {
			return physicalPublicationTx(ctx, conn, func(tx *sql.Tx) error {
				// Owner model -> physical registry -> intent. This matches model
				// registration order; readers hold only the shared registry gate.
				var table mysqlstore.UUID
				if err := tx.QueryRowContext(ctx, "SELECT id FROM "+e.schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID)).Scan(&table); err != nil {
					return err
				}
				var epoch, expectedEpoch int64
				var gate string
				var active, version mysqlstore.NullUUID
				if err := tx.QueryRowContext(ctx, "SELECT epoch,write_state,active_operation,published_version FROM "+e.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID)).Scan(&epoch, &gate, &active, &version); err != nil {
					return err
				}
				state, err := e.physicalPublicationIdentity(ctx, tx, r, true)
				if err != nil {
					return err
				}
				if err := tx.QueryRowContext(ctx, "SELECT expected_epoch FROM "+e.schema+".data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID), mysqlstore.UUID(r.Operation.ID)).Scan(&expectedEpoch); err != nil {
					return err
				}
				if state != "verifying" || gate != "closed" || !active.Valid || uuid.UUID(active.UUID) != r.Operation.ID || epoch != expectedEpoch+1 || !physicalPublicationVersion(r.Operation.Condition, version) {
					return ErrPhysicalConflict
				}
				if _, err := publication.readOperation(ctx, tx, e.tenant, r.TableID, r.Operation); !errors.Is(err, sql.ErrNoRows) {
					if err != nil {
						return err
					}
					return ErrPhysicalConflict
				}
				owner := mutate
				if e.creation != nil {
					owner = func(tx *sql.Tx) error {
						if err := mutate(tx); err != nil {
							return err
						}
						return e.verifyCreationMetadataTx(ctx, tx, r.TableID, r.Operation.ID)
					}
				}
				next, err := publication.MutateAndPublishOperationTx(ctx, tx, e.tenant, r.TableID, r.Operation, owner)
				if err != nil {
					return err
				}
				if resultReceipt != nil {
					if err := resultReceipt(tx, false); err != nil {
						return err
					}
				}
				// Archive/retirement must not reopen physical writes. Those owner
				// operations require a separate closed/retired publication protocol.
				var archived bool
				if err := tx.QueryRowContext(ctx, "SELECT archived FROM "+e.schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID)).Scan(&archived); err != nil {
					return err
				}
				if archived {
					return ErrModelUnavailable
				}
				updated, err := tx.ExecContext(ctx, "UPDATE "+e.schema+".data_model_schema_change_intents SET state='published',last_error_code='',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND table_id=? AND operation_id=? AND state='verifying'", mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID), mysqlstore.UUID(r.Operation.ID))
				if err := physicalPublicationChanged(updated, err); err != nil {
					return err
				}
				updated, err = tx.ExecContext(ctx, "UPDATE "+e.schema+".data_model_physical_tables SET published_version=?,write_state='open',active_operation=NULL WHERE tenant_id=? AND table_id=? AND epoch=? AND write_state='closed' AND active_operation=? AND (published_version<=>?)", mysqlstore.UUID(next.Version), mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID), epoch, mysqlstore.UUID(r.Operation.ID), version)
				if err := physicalPublicationChanged(updated, err); err != nil {
					return err
				}
				result = next
				return nil
			})
		})
	})
	if err != nil {
		return contract.Publication{}, err
	}
	return result, nil
}

type physicalPublicationReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (e *PhysicalExecutor) physicalPublicationIdentity(ctx context.Context, q physicalPublicationReader, r PhysicalPublicationRequest, lock bool) (string, error) {
	query := "SELECT request_hash,actor_kind,actor_id,expected_version,state FROM " + e.schema + ".data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?"
	if lock {
		query += " FOR UPDATE"
	}
	var hash []byte
	var kind, actor, state string
	var expected mysqlstore.NullUUID
	if err := q.QueryRowContext(ctx, query, mysqlstore.UUID(e.tenant), mysqlstore.UUID(r.TableID), mysqlstore.UUID(r.Operation.ID)).Scan(&hash, &kind, &actor, &expected, &state); err != nil {
		return "", err
	}
	if !bytes.Equal(hash, r.Operation.RequestHash[:]) || kind != r.ActorKind || actor != r.ActorID || !physicalPublicationVersion(r.Operation.Condition, expected) {
		return "", ErrPhysicalConflict
	}
	return state, nil
}

func physicalPublicationVersion(c PublicationCondition, version mysqlstore.NullUUID) bool {
	return (c.Mode == PublicationMustBeAbsent && !version.Valid) || (c.Mode == PublicationExactVersion && version.Valid && uuid.UUID(version.UUID) == c.Version)
}

func physicalPublicationChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrPhysicalConflict
	}
	return nil
}

func physicalPublicationTx(ctx context.Context, conn *sql.Conn, fn func(*sql.Tx) error) (result error) {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, &mysqlstore.RollbackError{Cause: err})
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return &mysqlstore.CommitError{Cause: err}
	}
	return nil
}
