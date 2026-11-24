// Package authtest supplies explicit helpers for disposable shared-schema tests.
// Production services must use the auth owner's migration runner instead.
package authtest

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"path/filepath"
	"strings"
)

type database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ApplyIdentity(ctx context.Context, db database, directory string) error {
	var name string
	if err := db.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return err
	}
	if !strings.Contains(name, "test") && !strings.Contains(name, "_it_") {
		return fmt.Errorf("identity fixture requires an explicitly named disposable test database")
	}
	for _, file := range []string{"schema.sql", "schema_v2.sql", "schema_v3.sql", "schema_v4.sql", "schema_v5.sql", "schema_v5_seed.sql", "schema_v6.sql"} {
		data, err := os.ReadFile(filepath.Join(directory, file))
		if err != nil {
			return err
		}
		if _, err = db.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("identity fixture %s: %w", file, err)
		}
	}
	return nil
}
