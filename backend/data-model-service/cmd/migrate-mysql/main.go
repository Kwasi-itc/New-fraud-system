// migrate-mysql executes a reviewed data-model DDL plan. It is never called by
// application startup and does not select the application's runtime database.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("migrate-mysql", flag.ContinueOnError)
	flags.SetOutput(out)
	action := flags.String("action", "status", "status, up, verify, install-grants (locked accounts only), or validate (no connection)")
	definerUser := flags.String("definer-user", "", "dedicated locked definer; required for verify or install-grants")
	definerHost := flags.String("definer-host", "localhost", "definer account host for verify")
	accountPolicyPath := flags.String("account-policies", "", "reviewed account-policy JSON for verify, validate or install-grants (maximum 1 MiB)")
	path := flags.String("plan", "", "reviewed JSON migration plan (maximum 32 MiB)")
	address := flags.String("address", "", "MySQL host:port")
	user := flags.String("user", "", "restricted migration account")
	passwordEnv := flags.String("password-env", "DATA_MODEL_MYSQL_MIGRATION_PASSWORD", "environment variable containing password")
	ca := flags.String("ca", "", "optional PEM CA file; defaults to system trust")
	serverName := flags.String("server-name", "", "TLS server name; defaults to address hostname")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall migration deadline")
	insecure := flags.Bool("isolated-fixture", false, "plaintext only for the isolated mysql:3306 fixture")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *path == "" || (*action != "status" && *action != "up" && *action != "validate" && *action != "verify" && *action != "install-grants") || *timeout <= 0 {
		return errors.New("explicit plan, valid action and positive timeout required")
	}
	definer := installation.Definer{User: *definerUser, Host: *definerHost}
	if *accountPolicyPath != "" && *action != "verify" && *action != "validate" && *action != "install-grants" {
		return errors.New("account policies are supported only by verify, validate or install-grants")
	}
	if *action == "install-grants" && *accountPolicyPath == "" {
		return errors.New("install-grants requires an explicit reviewed account policy file")
	}
	if *action == "verify" || *action == "install-grants" {
		if _, err := definer.SQLAccount(); err != nil {
			return err
		}
	}
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return errors.New("migration plan exceeds 32 MiB")
	}
	plan, err := decodePlan(data)
	if err != nil {
		return err
	}
	var policies []installation.AccountPolicy
	if *accountPolicyPath != "" {
		policies, err = readAccountPolicies(*accountPolicyPath, plan.Database)
		if err != nil {
			return err
		}
	}
	if *action == "install-grants" {
		for _, policy := range policies {
			if !policy.Locked || (policy.User == definer.User && policy.Host == definer.Host) {
				return errors.New("install-grants requires locked accounts distinct from the installed definer")
			}
		}
	}
	if *action == "validate" {
		_, err = fmt.Fprintf(out, "valid plan: database=%s steps=%d; no database inspected\n", plan.Database, len(plan.Steps))
		return err
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return errors.New("valid MySQL host:port required")
	}
	password, ok := os.LookupEnv(*passwordEnv)
	if !ok || password == "" {
		return errors.New("migration password environment variable is missing or empty")
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
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: *address, Database: plan.Database, User: *user, Password: password, TLS: tlsConfig, AllowInsecureTransport: *insecure, MaxOpen: 1, MaxIdle: 0, ConnectTimeout: 5 * time.Second, ReadTimeout: *timeout, WriteTimeout: 30 * time.Second, ConnMaxLifetime: *timeout, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return err
	}
	defer db.Close()
	runner, err := migrations.New(db, plan)
	if err != nil {
		return err
	}
	if *action == "up" {
		if err := runner.Apply(ctx); err != nil {
			return err
		}
	}
	if *action == "verify" || *action == "install-grants" {
		if err := runner.Verify(ctx, func(conn *sql.Conn) error {
			if err := installation.VerifyDefiner(ctx, conn, plan.Database, definer); err != nil {
				return err
			}
			if *action == "install-grants" {
				return installation.InstallLockedAccountPolicies(ctx, conn, plan.Database, policies)
			}
			for _, policy := range policies {
				if err := installation.VerifyAccountPolicy(ctx, conn, plan.Database, policy); err != nil {
					return fmt.Errorf("verify account %s@%s: %w", policy.User, policy.Host, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if *action == "install-grants" {
			_, err := fmt.Fprintf(out, "installed and verified reviewed grants on %d locked accounts; accounts remain locked; runtime not activated\n", len(policies))
			return err
		}
		_, err := fmt.Fprintf(out, "verified complete schema lineage and locked definer grants; additional reviewed accounts=%d; runtime not activated\n", len(policies))
		return err
	}
	history, err := runner.Status(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(history)
}
