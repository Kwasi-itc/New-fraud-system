package mysqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

var ErrDDLLockBusy = errors.New("DDL lock is held by another connection")

// DDLLockCleanupError means lock release was not confirmed. The connection is
// discarded, but callers still need to reconcile any DDL outcome before retrying.
type DDLLockCleanupError struct{ Cause error }

func (e *DDLLockCleanupError) Error() string {
	return "DDL lock cleanup failed; reconcile operation outcome"
}
func (e *DDLLockCleanupError) Unwrap() error { return e.Cause }

// WithDDLLock tries once to acquire a server-local named lock, then executes fn
// on that same pinned connection. Names must be stable, application-namespaced
// lowercase ASCII identifiers, at most 64 bytes; they are bound as SQL values.
// fn must use this connection synchronously for DDL/inspection, close its rows,
// and must not close the connection, manipulate locks or retain it after return.
//
// This is not a transaction or a DDL publication/recovery protocol. DDL may
// commit even if fn returns an error. No callback or acquisition is retried.
// Use a separately budgeted DDL pool; every borrowed connection is discarded,
// even after successful release, to isolate DDL session state from later work.
func WithDDLLock(ctx context.Context, db *sql.DB, name string, fn func(*sql.Conn) error) (result error) {
	if db == nil || fn == nil || !identifier.MatchString(name) {
		return errors.New("database, callback and valid DDL lock name are required")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire DDL connection: %w", err)
	}
	acquired := false
	defer func() {
		if acquired {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var released sql.NullInt64
			err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", name).Scan(&released)
			if err == nil && (!released.Valid || released.Int64 != 1) {
				err = errors.New("server did not confirm DDL lock ownership on release")
			}
			if err != nil {
				result = errors.Join(result, &DDLLockCleanupError{Cause: err})
			}
		}
		// ErrBadConn tells database/sql to destroy rather than pool this session.
		// This also covers uncertain acquisition and panic/cancellation paths.
		err := conn.Raw(func(any) error { return driver.ErrBadConn })
		if err != nil && !errors.Is(err, driver.ErrBadConn) && !errors.Is(err, sql.ErrConnDone) {
			result = errors.Join(result, &DDLLockCleanupError{Cause: err})
		}
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			result = errors.Join(result, &DDLLockCleanupError{Cause: err})
		}
	}()
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&locked); err != nil {
		return fmt.Errorf("acquire DDL lock: %w", err)
	}
	if !locked.Valid || (locked.Int64 != 0 && locked.Int64 != 1) {
		return errors.New("server did not confirm DDL lock acquisition")
	}
	if locked.Int64 == 0 {
		return ErrDDLLockBusy
	}
	acquired = true
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fn(conn); err != nil {
		return err
	}
	return ctx.Err()
}
