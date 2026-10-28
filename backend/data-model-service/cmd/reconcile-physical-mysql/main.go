// reconcile-physical-mysql resumes a bounded page of existing physical intents.
// It is an explicit operator command, never application startup or publication.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"time"

	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) (result error) {
	flags := flag.NewFlagSet("reconcile-physical-mysql", flag.ContinueOnError)
	flags.SetOutput(out)
	address := flags.String("address", "", "MySQL host:port")
	database := flags.String("database", "", "shared logical application database")
	user := flags.String("user", "", "restricted physical recovery account")
	passwordEnv := flags.String("password-env", "DATA_MODEL_MYSQL_RECOVERY_PASSWORD", "environment variable containing password")
	tenantText := flags.String("tenant", "", "explicit authorized tenant UUID")
	state := flags.String("state", "", "explicit prepared, applying, verifying or blocked state")
	afterText := flags.String("after", uuid.Nil.String(), "exclusive operation UUID cursor for this state")
	limit := flags.Int("limit", 100, "maximum attempts, 1..1000")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall deadline")
	operationTimeout := flags.Duration("operation-timeout", time.Minute, "per-operation deadline")
	ca := flags.String("ca", "", "optional PEM CA file; defaults to system trust")
	serverName := flags.String("server-name", "", "TLS server name; defaults to address hostname")
	insecure := flags.Bool("isolated-fixture", false, "plaintext only for the isolated mysql:3306 fixture")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *limit < 1 || *limit > 1000 || *timeout <= 0 || *operationTimeout <= 0 || *operationTimeout > *timeout || *user == "" {
		return errors.New("explicit account, bounded page and positive deadlines required; operation deadline must not exceed overall deadline")
	}
	switch *state {
	case "prepared", "applying", "verifying", "blocked":
	default:
		return errors.New("explicit pending intent state required")
	}
	tenant, err := uuid.Parse(*tenantText)
	if err != nil || tenant == uuid.Nil {
		return errors.New("explicit nonzero tenant UUID required")
	}
	after, err := uuid.Parse(*afterText)
	if err != nil {
		return errors.New("invalid operation cursor UUID")
	}
	if _, err := mysqlstore.Identifier(*database); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || host == "" {
		return errors.New("valid MySQL host:port required")
	}
	password, ok := os.LookupEnv(*passwordEnv)
	if !ok || password == "" {
		return errors.New("recovery password environment variable is missing or empty")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: *serverName}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = host
	}
	if *ca != "" {
		pem, err := os.ReadFile(*ca)
		if err != nil {
			return err
		}
		tlsConfig.RootCAs = x509.NewCertPool()
		if !tlsConfig.RootCAs.AppendCertsFromPEM(pem) {
			return errors.New("CA file has no valid certificates")
		}
	}
	if *insecure {
		if *address != "mysql:3306" || os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") != "1" {
			return errors.New("plaintext allowed only in explicitly configured disposable fixture")
		}
		tlsConfig = nil
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: *address, Database: *database, User: *user, Password: password, TLS: tlsConfig, AllowInsecureTransport: *insecure, MaxOpen: 1, MaxIdle: 0, ConnectTimeout: 5 * time.Second, ReadTimeout: *operationTimeout, WriteTimeout: 30 * time.Second, ConnMaxLifetime: *timeout, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	executor, err := store.NewPhysicalExecutor(db, *database, tenant)
	if err != nil {
		return err
	}
	page, recoveryErr := executor.ReconcilePage(ctx, *state, after, *limit, *operationTimeout)
	// Emit partial progress even on failure. A nonzero exit and the error must
	// never be erased merely because another operation advanced successfully.
	return errors.Join(recoveryErr, json.NewEncoder(out).Encode(page))
}
