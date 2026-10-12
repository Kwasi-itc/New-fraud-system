// identity-preflight prints one bounded page of existing operator references.
// It never creates accounts, updates case data or applies migrations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/preflight"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "identity-preflight:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("identity-preflight", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	tenant := flags.String("tenant", "", "required tenant UUID")
	source := flags.String("source", "", "required source from -list-sources")
	after := flags.String("after", "", "record UUID from next_after; keep tenant/source unchanged")
	limit := flags.Int("limit", 200, "page size, 1..1000")
	list := flags.Bool("list-sources", false, "list supported sources without connecting")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid arguments; use -help")
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *list {
		return json.NewEncoder(out).Encode(preflight.Sources())
	}
	id, err := uuid.Parse(*tenant)
	if err != nil {
		return errors.New("tenant must be a UUID")
	}
	r := preflight.Request{TenantID: id, Source: *source, Limit: *limit}
	if *after != "" {
		cursor, err := uuid.Parse(*after)
		if err != nil {
			return errors.New("after must be a record UUID")
		}
		r.After = &cursor
	}
	if err := r.Validate(); err != nil {
		return err
	}
	dsn := getenv("AUTH_PREFLIGHT_DATABASE_URL")
	if dsn == "" {
		return errors.New("AUTH_PREFLIGHT_DATABASE_URL is required; use an authorized read-only DB role")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	cfg.ConnectTimeout = 5 * time.Second
	cfg.RuntimeParams["application_name"] = "auth-identity-preflight"
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return errors.New("database connection failed; check connectivity and read-only credentials")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = conn.Close(cleanup)
	}()
	p, err := preflight.ReadPage(ctx, conn, r)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(p); err != nil {
		return errors.New("cannot write inventory output")
	}
	return nil
}
