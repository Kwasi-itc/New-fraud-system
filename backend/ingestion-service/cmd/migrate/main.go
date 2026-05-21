package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/app"
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
		MigrationsTable: "schema_migrations_ingestion",
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
	case "down":
		if err := m.Steps(-1); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate down: %v", err)
		}
	default:
		log.Fatalf("unsupported migrate command %q", os.Args[1])
	}

	fmt.Println("migration command completed")
}
