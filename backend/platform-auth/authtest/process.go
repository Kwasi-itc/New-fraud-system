package authtest

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/jackc/pgx/v5/pgxpool"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// StartAPI launches an actual newly built API against the disposable fixture.
// Each process receives its own signing key and explicit incoming public trust.
func StartAPI(t testing.TB, binary string, db *pgxpool.Pool, service string, extra []auth.TrustedKey, settings map[string]string) string {
	return StartAPIWithSigner(t, binary, db, ServiceSigner(t, service), extra, settings)
}

// StartAPIWithSigner lets a multi-process fixture distribute a caller's public
// trust before startup, without giving another process its private key.
func StartAPIWithSigner(t testing.TB, binary string, db *pgxpool.Pool, signer *auth.Signer, extra []auth.TrustedKey, settings map[string]string) string {
	t.Helper()
	if !strings.Contains(db.Config().ConnConfig.Database, "test") {
		t.Fatal("process fixture requires an isolated test database")
	}
	service := signer.ServiceID
	keys := append([]auth.TrustedKey{ServiceTrust(t, signer)}, extra...)
	raw, err := json.Marshal(auth.KeyRing{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	trustFile, keyFile := filepath.Join(dir, "public.json"), filepath.Join(dir, "private.pem")
	private, err := x509.MarshalPKCS8PrivateKey(signer.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trustFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := db.Config().ConnConfig
	ssl := "disable"
	if config.TLSConfig != nil {
		ssl = "require"
	}
	dsn := url.URL{Scheme: "postgres", Host: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), Path: "/" + config.Database, User: url.UserPassword(config.User, config.Password), RawQuery: "sslmode=" + ssl}
	environment := map[string]string{"PORT": strconv.Itoa(port), "GIN_MODE": "release", "DATABASE_URL": dsn.String(), "DATA_MODEL_SERVICE_URL": "http://127.0.0.1:1", "INGESTION_SERVICE_URL": "http://127.0.0.1:1", "PLATFORM_AUTH_KEYS_FILE": trustFile, "PLATFORM_AUTH_AUDIENCE": "urn:fraud:" + service, "PLATFORM_AUTH_SIGNING_KEY_FILE": keyFile, "PLATFORM_AUTH_SIGNING_KEY_ID": signer.KeyID, "PLATFORM_AUTH_SERVICE_ID": signer.ServiceID}
	for k, v := range settings {
		environment[k] = v
	}
	command := exec.Command(binary)
	command.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, override := environment[key]; !override {
			command.Env = append(command.Env, entry)
		}
	}
	for k, v := range environment {
		command.Env = append(command.Env, k+"="+v)
	}
	log, err := os.Create(filepath.Join(dir, "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
		log.Close()
		if t.Failed() {
			data, err := os.ReadFile(filepath.Join(dir, "api.log"))
			if err != nil {
				t.Logf("cannot read %s process log: %v", service, err)
			} else {
				t.Logf("%s process log:\n%s", service, data)
			}
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get(base + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return base
			}
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(filepath.Join(dir, "api.log"))
			t.Fatalf("API startup failed: %s", data)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
