package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/app"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: go run ./cmd/migrate [up|down]")
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	migrationsPath := "file://" + filepath.ToSlash(filepath.Join("internal", "migrations", "metadata"))
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database connection: %v", err)
	}
	defer db.Close()

	driver, err := migratepostgres.WithInstance(db, &migratepostgres.Config{
		MigrationsTable: "schema_migrations_decision_engine",
	})
	if err != nil {
		log.Fatalf("create postgres migration driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(migrationsPath, "postgres", driver)
	if err != nil {
		log.Fatalf("create migrate client: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	switch os.Args[1] {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate up: %v", err)
		}
		if err := runRiverMigrations(context.Background(), cfg.DatabaseURL, rivermigrate.DirectionUp, nil); err != nil {
			log.Fatalf("river migrate up: %v", err)
		}
	case "down":
		if err := m.Steps(-1); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate down: %v", err)
		}
		if err := runRiverMigrations(context.Background(), cfg.DatabaseURL, rivermigrate.DirectionDown, nil); err != nil {
			log.Fatalf("river migrate down: %v", err)
		}
	case "force":
		if len(os.Args) < 3 {
			log.Fatalf("usage: go run ./cmd/migrate force <version>")
		}
		version, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatalf("parse force version: %v", err)
		}
		if err := m.Force(version); err != nil {
			log.Fatalf("migrate force: %v", err)
		}
	default:
		log.Fatalf("unsupported migrate command %q", os.Args[1])
	}

	fmt.Println("migration command completed")
}

func runRiverMigrations(ctx context.Context, databaseURL string, direction rivermigrate.Direction, opts *rivermigrate.MigrateOpts) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open river migration database: %w", err)
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, direction, opts); err != nil {
		return err
	}
	return nil
}
