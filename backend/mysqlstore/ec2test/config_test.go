package ec2test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

func profile(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	raw, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	c := Config{Address: "writer.cluster-test.us-east-1.rds.amazonaws.com:3306", AdminUser: "admin", CAFile: ca, AccountHost: "10.0.1.10/255.255.255.255", Origins: []string{"http://192.0.2.10:8082"}}
	c.ReferencePasswordFile = filepath.Join(dir, "reference-password")
	if err := os.WriteFile(c.ReferencePasswordFile, []byte("independent-reference-password-long-enough"), 0600); err != nil {
		t.Fatal(err)
	}
	save(t, c)
	return c
}
func save(t *testing.T, c Config) {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ec2.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(Environment, path)
}
func TestConfigurationFailsClosed(t *testing.T) {
	c := profile(t)
	for _, change := range []func(*Config){
		func(c *Config) { c.Address = "127.0.0.1:3306" }, func(c *Config) { c.Address = "writer.example.com:3306" },
		func(c *Config) { c.Address = "writer.rds.amazonaws.com:0" }, func(c *Config) { c.AccountHost = "%" },
		func(c *Config) { c.AccountHost = "10.0.0.0/255.255.0.0" }, func(c *Config) { c.AdminUser = "admin'" },
		func(c *Config) { c.Origins = []string{"http://example.com"} }, func(c *Config) { c.Origins = []string{"http://192.0.2.10/path"} },
	} {
		bad := c
		change(&bad)
		save(t, bad)
		if _, err := Load(); err == nil {
			t.Fatal("unsafe profile accepted")
		}
	}
}
func TestTargetUsesVerifiedTLSReferenceRemainsSeparate(t *testing.T) {
	c := profile(t)
	target := driver.NewConfig()
	target.User = "root"
	target.Passwd = " unchanged password "
	if err := Driver(target, false); err != nil {
		t.Fatal(err)
	}
	if target.Addr != c.Address || target.User != "admin" || target.Passwd != " unchanged password " || target.Net != "tcp" || target.TLS == nil || target.TLS.InsecureSkipVerify || target.TLS.MinVersion < tls.VersionTLS12 || target.TLS.ServerName != "writer.cluster-test.us-east-1.rds.amazonaws.com" {
		t.Fatal("target contract changed")
	}
	ref := driver.NewConfig()
	ref.User = "root"
	if err := Driver(ref, true); err != nil {
		t.Fatal(err)
	}
	if ref.Addr != "mysql-reference:3306" || ref.User != "root" || ref.TLS != nil || ref.Passwd != "independent-reference-password-long-enough" {
		t.Fatal("reference redirected to target")
	}
	host, err := InstallerHost()
	if err != nil || host != "10.0.1.10" {
		t.Fatal(host, err)
	}
	for _, version := range []string{"8.4.7", "8.4.8", "8.4.9-aurora"} {
		if !VersionSupported(version) {
			t.Fatal(version)
		}
	}
	for _, version := range []string{"8.0.43", "9.0.1", "8.4.invalid"} {
		if VersionSupported(version) {
			t.Fatal(version)
		}
	}
	c.CAFile = filepath.Join(t.TempDir(), "missing")
	save(t, c)
	if err := Driver(driver.NewConfig(), false); err == nil {
		t.Fatal("missing CA accepted")
	}
}
func TestOwnerBoundaryDoesNotInjectIdentity(t *testing.T) {
	profile(t)
	calls := 0
	h, err := RequirePresented(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer presented" {
			t.Fatal("identity changed")
		}
		w.WriteHeader(204)
	}), "http://192.0.2.10:8082")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, origin string
		status        int
	}{{"", "", 401}, {"Basic nope", "", 401}, {"Bearer presented", "", 204}, {"Bearer presented", "http://evil.example", 403}} {
		r := httptest.NewRequest("POST", "/v1/decisions", nil)
		r.Header.Set("Authorization", tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status %d, want %d", w.Code, tc.status)
		}
	}
	if calls != 1 {
		t.Fatal("unauthorized owner call")
	}
}
func TestDefaultAdapterDoesNotChangeLocalConnections(t *testing.T) {
	t.Setenv(Environment, "")
	c := driver.NewConfig()
	c.Net = "unix"
	c.Addr = "/original/socket"
	c.User = "root"
	if err := Driver(c, false); err != nil || c.Net != "unix" || c.Addr != "/original/socket" || c.User != "root" {
		t.Fatal("local behavior changed")
	}
	if !VersionSupported("8.4.8") || VersionSupported("8.4.7") {
		t.Fatal("local pin changed")
	}
}
