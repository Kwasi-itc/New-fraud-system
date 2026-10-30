// mysql-release is a separate owner release-verification tool. The existing migrate
// command remains PostgreSQL; no environment flag silently changes backends.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlrelease"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// Populated by the reviewed image build. Unstamped developer binaries can
// inspect artifacts/grants offline, but cannot inspect deployment targets.
var builtRevision, builtSourceSHA256 string

func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("mysql-release", flag.ContinueOnError)
	f.SetOutput(out)
	var identity mysqlrelease.Identity
	mode := f.String("mode", "artifact", "artifact | grants | verify (no migration writes or runtime activation)")
	path := f.String("artifact", "", "reviewed canonical release artifact")
	digest := f.String("sha256", "", "externally approved artifact checksum")
	f.StringVar(&identity.Database, "database", "", "approved shared logical database")
	f.StringVar(&identity.Definer, "definer", "", "dedicated locked localhost definer")
	f.StringVar(&identity.MySQLVersion, "mysql-version", "8.4.8", "exact rehearsed MySQL version")
	f.StringVar(&identity.Revision, "revision", "", "approved full Git revision")
	f.StringVar(&identity.SourceSHA256, "source-sha256", "", "approved clean source tree digest")
	f.StringVar(&identity.ImageDigest, "image", "", "approved immutable image digest")
	address := f.String("address", "", "approved writer host:port; no DSN accepted")
	server := f.String("server-uuid", "", "independently verified writer UUID")
	user := f.String("user", "", "installer/inspection account, separate from runtime")
	passwordEnv := f.String("password-env", "DECISION_INSTALLER_PASSWORD", "password environment variable name")
	caPath := f.String("ca", "", "trusted CA PEM for verified TLS")
	runtimeUser := f.String("runtime-user", "", "existing dedicated account for grants output")
	runtimeHost := f.String("runtime-host", "", "explicit literal runtime account host")
	role := f.String("role", "", "api | worker; owner-only grant plan, not applied")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	switch *mode {
	case "artifact", "grants", "verify":
	default:
		return errors.New("unsupported operation; runtime activation is not available")
	}
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	release, decodeErr := mysqlrelease.Decode(file, *digest, identity)
	if err := errors.Join(decodeErr, file.Close()); err != nil {
		return err
	}
	if *mode == "grants" {
		grants, err := mysqlrelease.OwnerGrants(identity.Database, *runtimeUser, *runtimeHost, *role)
		if err != nil {
			return err
		}
		for _, grant := range grants {
			if _, err := fmt.Fprintln(out, grant+";"); err != nil {
				return err
			}
		}
		return nil
	}
	if *mode == "artifact" {
		_, err := fmt.Fprintln(out, "PASS: exact decision artifact verified; no database accessed; activation remains blocked")
		return err
	}
	if builtRevision != identity.Revision || builtSourceSHA256 != identity.SourceSHA256 {
		return errors.New("installer build provenance differs from approved release identity")
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || host == "" {
		return errors.New("explicit writer host:port required")
	}
	if *caPath == "" {
		return errors.New("trusted CA file required")
	}
	pem, err := os.ReadFile(*caPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return errors.New("invalid trusted CA PEM")
	}
	password, ok := os.LookupEnv(*passwordEnv)
	if !ok || password == "" {
		return errors.New("installer password environment variable is unset or empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: *address, Database: identity.Database, User: *user, Password: password, TLS: &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS12}, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return err
	}
	operationErr := release.Verify(ctx, db, *server)
	if err := errors.Join(operationErr, db.Close()); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "PASS: decision schema "+*mode+" completed; shared-stack/runtime activation remains blocked")
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "decision release failed:", err)
		os.Exit(1)
	}
}
