// admin is an explicit operator tool. It never imports email-based identity links.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "auth admin: operation failed:", err)
		os.Exit(1)
	}
}
func run() error {
	action := flag.String("action", "", "migrate or provision")
	tenant := flag.String("tenant", "", "existing active tenant UUID")
	issuer := flag.String("issuer", "", "exact trusted OIDC issuer")
	subject := flag.String("subject", "", "verified provider subject")
	name := flag.String("name", "", "display name")
	roles := flag.String("roles", "case_investigator", "comma-separated platform roles")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("invalid database config")
	}
	cfg.MaxConns = 1
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database unavailable")
	}
	defer db.Close()
	s := platform.Store{DB: db}
	switch *action {
	case "migrate":
		if err = s.Migrate(ctx); err != nil {
			return fmt.Errorf("migration failed; verify prerequisites and schema compatibility")
		}
		fmt.Println("Identity schema ready")
	case "provision":
		id, err := uuid.Parse(*tenant)
		if err != nil {
			return fmt.Errorf("invalid tenant")
		}
		user, err := s.Provision(ctx, id, *issuer, *subject, *name, strings.Split(*roles, ","))
		if err != nil {
			return fmt.Errorf("provision failed; verify tenant, identity, roles and existing mapping")
		}
		fmt.Println("Platform user:", user)
	default:
		return fmt.Errorf("choose migrate or provision")
	}
	return nil
}
