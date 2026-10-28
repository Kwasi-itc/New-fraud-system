// redrive-delivery-mysql is an explicit privileged operator command. Its account
// must be authorized for the selected tenant; flags do not authorize API users.
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
	flags := flag.NewFlagSet("redrive-delivery-mysql", flag.ContinueOnError)
	flags.SetOutput(out)
	address := flags.String("address", "", "MySQL host:port")
	database := flags.String("database", "", "shared logical application database")
	user := flags.String("user", "", "authorized operator account")
	passwordEnv := flags.String("password-env", "DATA_MODEL_MYSQL_REDRIVE_PASSWORD", "password environment variable")
	tenantText := flags.String("tenant", "", "explicit authorized tenant UUID")
	jobText := flags.String("job", "", "pending job UUID")
	sourceText := flags.String("source", "", "discarded delivery UUID")
	requestText := flags.String("request", "", "stable request UUID; reuse all inputs after uncertain commit")
	actor := flags.String("actor", "", "operator identity attested by this privileged caller")
	reason := flags.String("reason", "", "required recovery reason, at most 2048 bytes")
	schedule := flags.String("scheduled-at", "", "explicit RFC3339 availability time, microsecond precision")
	queue := flags.String("queue", "", "source queue; cross-queue redrive is forbidden")
	attempts := flags.Int("max-attempts", 0, "explicit NEW delivery attempt budget; domain attempts are preserved")
	apply := flags.Bool("apply", false, "apply audited redrive; inspect first with inspect-deliveries-mysql")
	timeout := flags.Duration("timeout", 30*time.Second, "overall deadline")
	ca := flags.String("ca", "", "optional PEM CA file; defaults to system trust")
	serverName := flags.String("server-name", "", "TLS server name; defaults to address hostname")
	insecure := flags.Bool("isolated-fixture", false, "plaintext only for explicitly enabled mysql:3306 disposable fixture")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || !*apply || *user == "" || *timeout <= 0 || *attempts < 1 || *attempts > 2147483647 {
		return errors.New("apply, explicit account and attempt budget, and positive timeout required")
	}
	r := store.DeliveryRedriveRequest{Actor: *actor, Reason: *reason}
	for _, v := range []struct {
		text   string
		target *uuid.UUID
	}{{*tenantText, &r.TenantID}, {*jobText, &r.JobID}, {*sourceText, &r.SourceID}, {*requestText, &r.RequestID}} {
		id, err := uuid.Parse(v.text)
		if err != nil || id == uuid.Nil {
			return errors.New("nonzero tenant, job, source and request UUIDs required")
		}
		*v.target = id
	}
	var err error
	r.ScheduledAt, err = time.Parse(time.RFC3339Nano, *schedule)
	if err != nil {
		return errors.New("explicit RFC3339 schedule required")
	}
	if err := r.Validate(); err != nil {
		return err
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
		return errors.New("redrive password environment variable is missing or empty")
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

	deliveryStore, err := store.NewIndexDeliveryStore(db, *database, *queue, *attempts, time.Minute)
	if err != nil {
		return err
	}
	id, err := deliveryStore.RedriveDiscarded(ctx, r)
	if err != nil {
		return err
	}
	// This acknowledges a committed (or replayed) redrive, not successful job/DDL execution.
	return json.NewEncoder(out).Encode(struct {
		DeliveryID uuid.UUID `json:"delivery_id"`
		RequestID  uuid.UUID `json:"request_id"`
	}{id, r.RequestID})
}
