package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// Verify requires the complete applied lineage and exact latest object shapes.
// It never bootstraps a missing ledger, executes DDL or repairs dirty history.
// An optional inspector runs on the same pinned connection under the migration
// lock, for checks such as restricted definer grants. It must not retain/close
// the connection, manipulate locks or perform external network operations.
func (r *Runner) Verify(ctx context.Context, inspect func(*sql.Conn) error) error {
	return r.locked(ctx, func(conn *sql.Conn, schema string) error {
		return r.VerifyOnConn(ctx, conn, inspect)
	})
}

// VerifyOnConn performs the same read-only lineage and shape checks on a
// caller-owned pinned connection. The caller must exclude installation changes
// throughout verification and subsequent use, and retain its physical execution
// lock through publication. This method acquires no lock and opens no connection;
// it is not a reusable readiness certificate. In particular, it must not be used
// to verify now and publish later after releasing the caller's lock.
func (r *Runner) VerifyOnConn(ctx context.Context, conn *sql.Conn, inspect func(*sql.Conn) error) error {
	if conn == nil {
		return errors.New("pinned verification connection required")
	}
	schema, _ := mysqlstore.Identifier(r.plan.Database)
	actual, err := Definition(ctx, conn, r.plan.Database, "TABLE", "data_model_schema_migrations")
	if err != nil {
		return err
	}
	if actual == "" {
		return ErrLineage
	}
	want, _ := json.Marshal(map[string]string{"Table": "data_model_schema_migrations", "Create Table": ledgerCreate})
	if actual != string(want) {
		return ErrDrift
	}
	history, err := r.history(ctx, conn, schema)
	if err != nil {
		return err
	}
	if len(history) != len(r.plan.Steps) {
		return ErrLineage
	}
	latest := make(map[string]int, len(history))
	for i, h := range history {
		if h.State != "applied" {
			return ErrLineage
		}
		s := r.plan.Steps[i]
		latest[s.Kind+":"+s.Name] = i
	}
	for i, s := range r.plan.Steps {
		if latest[s.Kind+":"+s.Name] != i {
			continue
		}
		actual, err := Definition(ctx, conn, r.plan.Database, s.Kind, s.Name)
		if err != nil {
			return err
		}
		if actual != s.After {
			return fmt.Errorf("verify applied step %s: %w", s.ID, ErrDrift)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if inspect != nil {
		return inspect(conn)
	}
	return nil
}
