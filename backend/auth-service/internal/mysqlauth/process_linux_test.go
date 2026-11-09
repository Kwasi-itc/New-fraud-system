package mysqlauth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/google/uuid"
)

func smtpFixture(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	messages := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				fmt.Fprint(conn, "220 native.local ESMTP\r\n")
				reader := bufio.NewReader(conn)
				for {
					line, e := reader.ReadString('\n')
					if e != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
						fmt.Fprint(conn, "250-native.local\r\n250 HELP\r\n")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"), strings.HasPrefix(line, "RSET"):
						fmt.Fprint(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						fmt.Fprint(conn, "354 data\r\n")
						var b strings.Builder
						for {
							line, e = reader.ReadString('\n')
							if e != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
							b.WriteString(line)
						}
						messages <- b.String()
						fmt.Fprint(conn, "250 stored\r\n")
					case strings.HasPrefix(line, "QUIT"):
						fmt.Fprint(conn, "221 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "500 unsupported\r\n")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			t.Error("SMTP fixture did not stop")
		}
	})
	return listener.Addr().String(), messages
}
func freeAddress(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := l.Addr().String()
	l.Close()
	return address
}

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	logs bytes.Buffer
}

func startProcess(t *testing.T, binary, config, secrets, address string, worker bool) *process {
	t.Helper()
	args := []string{"--config", config, "--secrets-dir", secrets, "--listen", address}
	if worker {
		args = append(args, "--worker")
	}
	p := &process{done: make(chan struct{})}
	p.cmd = exec.Command(binary, args...)
	p.cmd.Env = append(os.Environ(), "AUTH_LOCAL_MYSQL=1", "AUTH_REQUIRE_MYSQL_TEST=1")
	p.cmd.Stdout = &p.logs
	p.cmd.Stderr = &p.logs
	if e := p.cmd.Start(); e != nil {
		t.Fatal(e)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
			p.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			p.cmd.Process.Kill()
			<-p.done
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	client := http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			t.Fatalf("auth process exited: %v %s", p.err, p.logs.String())
		default:
		}
		r, e := client.Get("http://" + address + "/readyz")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 204 {
				return p
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("auth process readiness timed out")
	return nil
}
func (p *process) stop(t *testing.T) {
	t.Helper()
	if e := p.cmd.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	select {
	case <-p.done:
		if p.err != nil {
			t.Fatalf("auth shutdown: %v %s", p.err, p.logs.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("auth process did not cancel and join")
	}
}

func TestMySQLAPIAndWorkerProcessesRestartAndDeliver(t *testing.T) {
	binary := os.Getenv("AUTH_MYSQL_PROCESS_BINARY")
	if binary == "" {
		if os.Getenv("AUTH_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required process binary missing")
		}
		t.Skip("build the native process fixture")
	}
	f := newFixture(t)
	_, issuer, issue := oidcFixture(t)
	smtp, messages := smtpFixture(t)
	directory := t.TempDir()
	apiSecrets, workerSecrets := filepath.Join(directory, "api"), filepath.Join(directory, "worker")
	for _, path := range []string{apiSecrets, workerSecrets} {
		if e := os.Mkdir(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	password := strings.Repeat("c", 64)
	for _, role := range []string{"api", "worker"} {
		user := "local_auth_" + role
		mustExec(t, f, "CREATE USER IF NOT EXISTS '"+user+"'@'%' IDENTIFIED BY '"+password+"'")
		mustExec(t, f, "REVOKE ALL PRIVILEGES,GRANT OPTION FROM '"+user+"'@'%'")
		t.Cleanup(func() {
			if _, e := f.db.Exec("DROP USER '" + user + "'@'%'"); e != nil {
				t.Error(e)
			}
		})
		grants, e := RuntimeGrants(f.database, user, role == "worker")
		if e != nil {
			t.Fatal(e)
		}
		for _, g := range grants {
			mustExec(t, f, g)
		}
	}
	write := func(dir, name, value string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(apiSecrets, "auth_api_password", password)
	write(workerSecrets, "auth_worker_password", password)
	encryption := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, dir := range []string{apiSecrets, workerSecrets} {
		write(dir, "auth_invitation_key", encryption)
	}
	proxy := strings.Repeat("d", 64)
	write(apiSecrets, "auth_proxy_token", proxy)
	write(apiSecrets, "auth_oidc_client_secret", "fixture-private-client")
	// Reuse a test signing key without generating or exposing any deployed key.
	privateKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	keyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if e = os.WriteFile(filepath.Join(apiSecrets, "auth_signing_key"), keyBytes, 0600); e != nil {
		t.Fatal(e)
	}
	c := LocalConfig{Address: "mysql:3306", Database: f.database, AppOrigin: "http://localhost:8087", InvitationOrigin: "http://localhost:3000", Issuer: "http://localhost:8087", Audience: "fraud-platform", KeyID: "native", DefaultTenantID: f.tenant, Provider: oidcprovider.Config{Environment: "development", IssuerURL: issuer, ClientID: "auth-fixture", RedirectURL: "http://localhost:3000/api/auth/callback"}}
	c.SMTP.Address = smtp
	c.SMTP.From = "auth@local.invalid"
	c.SMTP.LocalCapture = true
	data, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	config := filepath.Join(directory, "config.json")
	if e = os.WriteFile(config, data, 0600); e != nil {
		t.Fatal(e)
	}
	apiAddress, workerAddress := freeAddress(t), freeAddress(t)
	api := startProcess(t, binary, config, apiSecrets, apiAddress, false)
	worker := startProcess(t, binary, config, workerSecrets, workerAddress, true)
	if _, e = f.store.Provision(context.Background(), f.tenant, issuer, "process-admin", "Process Admin", []string{"tenant_admin", "platform_admin"}); e != nil {
		t.Fatal(e)
	}
	call := func(path string, body any, want int) map[string]any {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		request, e := http.NewRequest("POST", "http://"+apiAddress+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Authorization", "Bearer "+proxy)
		request.Header.Set("Content-Type", "application/json")
		client := http.Client{Timeout: 10 * time.Second}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		content, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("process %s returned %d want %d: %s", path, response.StatusCode, want, content)
		}
		out := map[string]any{}
		if len(content) > 0 {
			if e = json.Unmarshal(content, &out); e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	login := call("/internal/login/start", nil, 200)
	location := login["url"].(string)
	parsed, e := url.Parse(location)
	if e != nil {
		t.Fatal(e)
	}
	code := issue(location, oidcprovider.Identity{Subject: "process-admin", Name: "Process Admin"})
	raw := call("/internal/login/complete", map[string]any{"state": parsed.Query().Get("state"), "binding": login["binding"], "code": code}, 200)["session"].(string)
	call("/internal/session", map[string]any{"session": raw}, 200)
	invite := uuid.New()
	call("/internal/invitations/change", map[string]any{"session": raw, "action": "create", "id": invite, "email": "Process@example.test", "roles": []string{"platform_viewer"}}, 204)
	select {
	case message := <-messages:
		if !strings.Contains(message, invite.String()+".1") || !strings.Contains(message, "/invite#") {
			t.Fatal("SMTP message binding missing")
		}
	case <-time.After(12 * time.Second):
		t.Fatal("real worker did not deliver SMTP")
	}
	deadline := time.Now().Add(5 * time.Second)
	state := ""
	for time.Now().Before(deadline) {
		if e = f.db.QueryRow("SELECT state FROM "+f.store.table("identity_invitation_delivery")+" WHERE invitation_id=?", id(invite)).Scan(&state); e != nil {
			t.Fatal(e)
		}
		if state == "sent" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if state != "sent" {
		t.Fatal("delivery receipt not persisted", state)
	}
	worker.stop(t)
	worker = startProcess(t, binary, config, workerSecrets, workerAddress, true)
	worker.stop(t)
	api.stop(t)
	api = startProcess(t, binary, config, apiSecrets, apiAddress, false)
	call("/internal/session", map[string]any{"session": raw}, 200)
	api.stop(t)
}
