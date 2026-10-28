// inspect-deliveries-mysql is a read-only, explicitly tenant-scoped operator
// command. It does not authorize API users, recover work or activate runtime.
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
	flags := flag.NewFlagSet("inspect-deliveries-mysql", flag.ContinueOnError)
	flags.SetOutput(out)
	address := flags.String("address", "", "MySQL host:port")
	database := flags.String("database", "", "shared logical application database")
	user := flags.String("user", "", "authorized read-only inspection account")
	passwordEnv := flags.String("password-env", "DATA_MODEL_MYSQL_INSPECTION_PASSWORD", "password environment variable")
	tenantText := flags.String("tenant", "", "explicit authorized tenant UUID")
	deliveryText := flags.String("delivery", "", "optional delivery UUID for one detail record")
	beforeID := flags.String("before-id", "", "exclusive page cursor UUID; requires before-created-at")
	beforeTime := flags.String("before-created-at", "", "exclusive cursor time in RFC3339 with microsecond precision")
	limit := flags.Int("limit", 100, "page size 1..1000, across all tenant queues")
	timeout := flags.Duration("timeout", 30*time.Second, "overall inspection deadline")
	ca := flags.String("ca", "", "optional PEM CA file; defaults to system trust")
	serverName := flags.String("server-name", "", "TLS server name; defaults to address hostname")
	insecure := flags.Bool("isolated-fixture", false, "plaintext only for explicitly enabled mysql:3306 disposable fixture")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *user == "" || *limit < 1 || *limit > 1000 || *timeout <= 0 {
		return errors.New("explicit account, bounded page and positive timeout required")
	}
	tenant, err := uuid.Parse(*tenantText)
	if err != nil || tenant == uuid.Nil {
		return errors.New("explicit nonzero tenant UUID required")
	}
	if _, err := mysqlstore.Identifier(*database); err != nil {
		return err
	}
	var delivery uuid.UUID
	if *deliveryText != "" {
		delivery, err = uuid.Parse(*deliveryText)
		if err != nil || delivery == uuid.Nil {
			return errors.New("nonzero delivery UUID required")
		}
	}
	var cursor *store.DeliveryInspectionCursor
	if *beforeID != "" || *beforeTime != "" {
		if delivery != uuid.Nil {
			return errors.New("detail lookup cannot use a page cursor")
		}
		id, err := uuid.Parse(*beforeID)
		if err != nil || id == uuid.Nil {
			return errors.New("nonzero cursor UUID and timestamp required")
		}
		at, err := time.Parse(time.RFC3339Nano, *beforeTime)
		if err != nil {
			return errors.New("RFC3339 cursor timestamp required")
		}
		if _, err := (mysqlstore.DateTime{Time: at}).Value(); err != nil {
			return err
		}
		cursor = &store.DeliveryInspectionCursor{TenantID: tenant, CreatedAt: at, ID: id}
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || host == "" {
		return errors.New("valid MySQL host:port required")
	}
	password, ok := os.LookupEnv(*passwordEnv)
	if !ok || password == "" {
		return errors.New("inspection password environment variable is missing or empty")
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
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: *address, Database: *database, User: *user, Password: password, TLS: tlsConfig, AllowInsecureTransport: *insecure, MaxOpen: 1, MaxIdle: 0, ConnectTimeout: 5 * time.Second, ReadTimeout: *timeout, WriteTimeout: *timeout, ConnMaxLifetime: *timeout, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	inspector, err := store.NewDeliveryInspector(db, *database, tenant)
	if err != nil {
		return err
	}
	if delivery != uuid.Nil {
		item, err := inspector.Get(ctx, delivery)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(item)
	}
	page, err := inspector.Page(ctx, cursor, *limit)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(page)
}
