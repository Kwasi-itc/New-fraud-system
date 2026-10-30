package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

const processToken = "isolated-runtime-secret-token-2026"

type testProcess struct {
	cmd          *exec.Cmd
	done         chan struct{}
	err          error
	address, log string
}

func (p *testProcess) stop(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		if p.err != nil {
			data, _ := os.ReadFile(p.log)
			t.Errorf("process exited: %v %s", p.err, data)
		}
		return
	default:
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Error(err)
	}
	select {
	case <-p.done:
		if p.err != nil {
			data, _ := os.ReadFile(p.log)
			t.Errorf("process shutdown: %v %s", p.err, data)
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		t.Error("process failed to join shutdown")
	}
}
func startProcess(t *testing.T, config map[string]any, worker bool) *testProcess {
	t.Helper()
	binary := "/tmp/ingestion-mysql-api"
	if worker {
		binary = "/tmp/ingestion-mysql-worker"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatal("build required process binary", err)
	}
	dir := t.TempDir()
	wire, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err = os.WriteFile(path, wire, 0600); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")
	log := filepath.Join(dir, "process.log")
	output, err := os.Create(log)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-config", path, "-ready-file", ready)
	cmd.Env = append(os.Environ(), "INGESTION_RUNTIME_TOKEN="+processToken, "INGESTION_RUNTIME_PASSWORD=isolated-reader-only", "GIN_MODE=release")
	cmd.Stdout = output
	cmd.Stderr = output
	p := &testProcess{cmd: cmd, done: make(chan struct{}), log: log}
	if err = cmd.Start(); err != nil {
		output.Close()
		t.Fatal(err)
	}
	go func() { p.err = cmd.Wait(); output.Close(); close(p.done) }()
	t.Cleanup(func() { p.stop(t) })
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if data, e := os.ReadFile(ready); e == nil {
			p.address = "http://" + string(data)
			return p
		}
		select {
		case <-p.done:
			data, _ := os.ReadFile(log)
			t.Fatalf("startup failed: %v %s", p.err, data)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("readiness timed out")
	return nil
}
func processFixture(t *testing.T) (sharedFixture, recordFixtureState, map[string]any) {
	t.Helper()
	shared, f := fullRecordFixture(t)
	t.Cleanup(func() { reconcileProcessFixture(t, f) })
	owner, pool := fullOwner(t, shared, f, "worker")
	var user string
	if err := pool.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&user); err != nil {
		t.Fatal(err)
	}
	grants, err := PrivateExecutionGrants(shared.Database, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		fixtureExec(t, f.db, q)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	cfg := map[string]any{"Authority": "isolated-mysql", "Address": "mysql:3306", "Database": shared.Database, "User": user, "TenantID": f.model.TenantID, "ObjectType": "records", "UploadQueue": "uploads_" + suffix, "DeferredQueue": "deferred_" + suffix, "Pool": 16, "Concurrency": 2, "WriteConcurrency": 32, "Attempts": 2, "BodyBytes": 1 << 20, "ContractBytes": 1 << 20, "Limits": owner.limits, "Lease": int64(3 * time.Second), "Timeout": int64(2 * time.Second), "Poll": int64(20 * time.Millisecond), "Shutdown": int64(5 * time.Second), "IngestionPlan": shared.Ingestion, "DataModelPlan": shared.DataModel}
	return shared, f, cfg
}
func processRequest(ctx context.Context, p *testProcess, method, path, key string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, p.address+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+processToken)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return response.StatusCode, data, err
}
func recordPath(f recordFixtureState) string {
	return "/v1/tenants/" + f.model.TenantID.String() + "/ingest/records"
}
func expectHTTP(t *testing.T, p *testProcess, method, path, key, body string, want int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, data, err := processRequest(ctx, p, method, path, key, []byte(body))
	if err != nil || status != want {
		t.Fatalf("HTTP %s %s: status=%d want=%d error=%v body=%s", method, path, status, want, err, data)
	}
	return data
}
func waitWorkState(t *testing.T, db *sql.DB, id uuid.UUID, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var state string
	for time.Now().Before(deadline) {
		err := db.QueryRow("SELECT state FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&state)
		if err != nil {
			t.Fatal(err)
		}
		if state == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("work %s state=%s want=%s", id, state, want))
}
func TestProcessWriteBaseline(t *testing.T) {
	_, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	path := recordPath(f)
	expectHTTP(t, p, "GET", "/readyz", "", "", 200)
	data := expectHTTP(t, p, "POST", path, "first", `{"object_id":"one","number":1}`, 200)
	var first struct{ Result map[string]any }
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatal(err)
	}
	if first.Result["object_id"] != "one" || first.Result["action"] != "created" || first.Result["replayed"] != false {
		t.Fatal(first)
	}
	replay := expectHTTP(t, p, "POST", path, "first", `{"object_id":"one","number":1}`, 200)
	if !bytes.Contains(replay, []byte(`"replayed":true`)) {
		t.Fatal(string(replay))
	}
	expectHTTP(t, p, "PATCH", path, "patch", `{"object_id":"one","flag":true}`, 200)
	expectHTTP(t, p, "POST", path+"/batch", "batch", `[{"object_id":"two","number":2},{"object_id":"three","number":3}]`, 200)
	expectHTTP(t, p, "PATCH", path+"/batch", "batch-patch", `[{"object_id":"three","flag":false},{"object_id":"two","amount":2.5}]`, 200)
	expectHTTP(t, p, "POST", path, "first", `{"object_id":"changed","number":1}`, 409)
	expectHTTP(t, p, "PATCH", path, "conflict", `{"object_id":"other","number":1}`, 409)
	expectHTTP(t, p, "POST", path, "invalid", `{"object_id":"bad","number":"bad"}`, 422)
	expectHTTP(t, p, "POST", path+"/batch", "empty", `[]`, 400)
	expectHTTP(t, p, "POST", strings.Replace(path, f.model.TenantID.String(), uuid.NewString(), 1), "foreign", `{"object_id":"one"}`, 403)
	expectHTTP(t, p, "GET", "/v1/upload-logs/"+uuid.NewString(), "", "", 404)
	expectHTTP(t, p, "GET", "/v1/deferred-ingests/"+uuid.NewString(), "", "", 404)
	// Independent unkeyed requests are distinct operations in the private namespace.
	expectHTTP(t, p, "PATCH", path, "", `{"object_id":"one","flag":true}`, 200)
	expectHTTP(t, p, "PATCH", path, "", `{"object_id":"one","flag":true}`, 200)
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM ingestion_execution_receipts").Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
