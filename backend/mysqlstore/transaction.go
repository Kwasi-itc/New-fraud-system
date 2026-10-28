package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CommitError requires operation-key reconciliation. A connection error during
// COMMIT may mean the server committed; callers must not blindly retry it.
type CommitError struct{ Cause error }

func (e *CommitError) Error() string { return "transaction commit failed; reconcile operation outcome" }
func (e *CommitError) Unwrap() error { return e.Cause }

// RollbackError means rollback cleanup was not confirmed. It is joined with
// the original callback error; callers must not infer retry safety from that
// callback error alone. This does not imply that a COMMIT was sent.
type RollbackError struct{ Cause error }

func (e *RollbackError) Error() string { return "transaction rollback was not confirmed" }
func (e *RollbackError) Unwrap() error { return e.Cause }

// Mutation executes one READ COMMITTED transaction, without automatic retries.
// Callbacks must not commit, roll back, execute DDL/session changes, or make
// network calls. Repositories own business rules and return their original error.
func Mutation(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	return transaction(ctx, db, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, fn)
}

// Snapshot pins all ordinary reads to the snapshot established by the first
// consistent SELECT. Locking reads are current reads and must not be used here.
func Snapshot(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	return transaction(ctx, db, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}, fn)
}

func transaction(ctx context.Context, db *sql.DB, options *sql.TxOptions, fn func(*sql.Tx) error) (result error) {
	if db == nil || fn == nil {
		return errors.New("database and transaction callback are required")
	}
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Rollback has no canceled request context. Driver socket timeouts bound
		// network cleanup; database/sql also rolls back canceled transactions.
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, &RollbackError{Cause: err})
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return &CommitError{Cause: err}
	}
	return nil
}
