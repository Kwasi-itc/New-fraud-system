package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// PreparePhysicalFieldChange permits only session-local temporary-table DDL
// alongside the transactional writer barrier and intent. Unlike application DDL,
// CREATE/DROP TEMPORARY TABLE does not commit MySQL transactions. The dedicated
// connection is discarded even on cancellation: a canceled Tx can no longer
// drop its temporary tables, and returning it to the pool would leak that state.
func PreparePhysicalFieldChange(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) (result error) {
	if db == nil || fn == nil {
		return errors.New("database and preparation callback required")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := conn.Raw(func(any) error { return driver.ErrBadConn })
		if err != nil && !errors.Is(err, driver.ErrBadConn) && !errors.Is(err, sql.ErrConnDone) {
			result = errors.Join(result, err)
		}
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			result = errors.Join(result, err)
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, &mysqlstore.RollbackError{Cause: err})
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return &mysqlstore.CommitError{Cause: err}
	}
	return nil
}
