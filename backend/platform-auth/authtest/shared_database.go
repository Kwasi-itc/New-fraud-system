package authtest

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// SharedDatabase applies real owner migrations to a newly created disposable
// database. The supplied database is only used for CREATE/DROP DATABASE.
func SharedDatabase(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("AUTH_PREFLIGHT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AUTH_PREFLIGHT_TEST_DATABASE_URL required for isolated shared-schema tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "platform_auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	var db *pgxpool.Pool
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if db != nil {
			db.Close()
		}
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(cleanup)
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 8
	db, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	backend := filepath.Join(filepath.Dir(source), "..", "..")
	for _, owner := range []string{"data-model-service", "identity", "ingestion-service", "decision-engine-service", "screening-service", "case-manager-service"} {
		if owner == "identity" {
			if err := ApplyIdentity(ctx, db, filepath.Join(backend, "auth-service", "internal", "platform")); err != nil {
				t.Fatal(err)
			}
			continue
		}
		files, err := filepath.Glob(filepath.Join(backend, owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(files) == 0 {
			t.Fatal("owner migrations missing", owner, err)
		}
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, string(raw)); err != nil {
				t.Fatalf("%s: %v", file, err)
			}
		}
	}
	return db
}
