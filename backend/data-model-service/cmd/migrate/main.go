package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/app"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./cmd/migrate [up|down|version] [steps]")
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("get working directory: %v", err)
	}
	migrationsPath := "file://" + filepath.ToSlash(filepath.Join(wd, "internal", "migrations", "metadata"))

	m, err := migrate.New(migrationsPath, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("create migrate client: %v", err)
	}

	cmd := os.Args[1]
	switch cmd {
	case "up":
		err = m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate up: %v", err)
		}
		if err := runRiverMigrations(context.Background(), cfg.DatabaseURL, rivermigrate.DirectionUp, nil); err != nil {
			log.Fatalf("river migrate up: %v", err)
		}
	case "down":
		steps := 1
		if len(os.Args) > 2 {
			steps, err = strconv.Atoi(os.Args[2])
			if err != nil {
				log.Fatalf("parse down steps: %v", err)
			}
		}
		err = m.Steps(-steps)
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate down: %v", err)
		}
		if err := runRiverMigrations(context.Background(), cfg.DatabaseURL, rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{MaxSteps: steps}); err != nil {
			log.Fatalf("river migrate down: %v", err)
		}
	case "version":
		version, dirty, err := m.Version()
		if err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				fmt.Println("version: none")
				return
			}
			log.Fatalf("migrate version: %v", err)
		}
		fmt.Printf("version: %d dirty: %t\n", version, dirty)
	default:
		log.Fatalf("unknown command: %s", cmd)
	}
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
