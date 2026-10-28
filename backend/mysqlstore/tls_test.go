package mysqlstore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"testing"
	"time"
)

func TestMySQLVerifiedTLS(t *testing.T) {
	address := testAddress(t)
	path := os.Getenv("MYSQLSTORE_TEST_CA")
	if path == "" {
		if os.Getenv("MYSQLSTORE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("required test CA missing")
		}
		t.Skip("test CA not configured")
	}
	cert, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("invalid test CA")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := testConfig()
	c.Address = address
	c.Password = testPassword()
	c.TLS = &tls.Config{RootCAs: roots, ServerName: "mysql"}
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var variable, cipher string
	if err := db.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Ssl_cipher'").Scan(&variable, &cipher); err != nil || cipher == "" {
		t.Fatalf("TLS inactive: %q %v", cipher, err)
	}
	c.TLS = &tls.Config{RootCAs: roots, ServerName: "wrong-host.invalid"}
	bad, err := Open(ctx, c)
	if bad != nil {
		bad.Close()
	}
	var hostname x509.HostnameError
	if !errors.As(err, &hostname) {
		t.Fatalf("expected hostname rejection: %v", err)
	}
	c.TLS = &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "mysql"}
	bad, err = Open(ctx, c)
	if bad != nil {
		bad.Close()
	}
	var authority x509.UnknownAuthorityError
	if !errors.As(err, &authority) {
		t.Fatalf("expected CA rejection: %v", err)
	}
}
