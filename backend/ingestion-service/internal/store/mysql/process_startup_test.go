package mysql

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func rejectedProcess(t *testing.T, cfg map[string]any, worker bool) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	ready := filepath.Join(dir, "ready")
	data, e := json.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	binary := "/tmp/ingestion-mysql-api"
	if worker {
		binary = "/tmp/ingestion-mysql-worker"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-config", path, "-ready-file", ready)
	cmd.Env = append(os.Environ(), "INGESTION_RUNTIME_TOKEN="+processToken, "INGESTION_RUNTIME_PASSWORD=isolated-reader-only")
	output, e := cmd.CombinedOutput()
	if e == nil || ctx.Err() != nil {
		t.Fatalf("expected bounded startup rejection: %v %s", e, output)
	}
	if _, e = os.Stat(ready); !os.IsNotExist(e) {
		t.Fatal("rejected process published readiness", e)
	}
}
func TestProcessStartupChecks(t *testing.T) {
	shared, f, cfg := processFixture(t)
	for _, worker := range []bool{false, true} {
		original := cfg["Authority"]
		cfg["Authority"] = "mysql"
		rejectedProcess(t, cfg, worker)
		cfg["Authority"] = original
		cfg["Database"] = "fraud"
		rejectedProcess(t, cfg, worker)
		cfg["Database"] = shared.Database
		user := cfg["User"].(string)
		cfg["User"] = "root"
		rejectedProcess(t, cfg, worker)
		cfg["User"] = user
		pool := cfg["Pool"]
		cfg["Pool"] = 1
		rejectedProcess(t, cfg, worker)
		cfg["Pool"] = pool
		fixtureExec(t, f.db, "REVOKE INSERT ON "+shared.Database+".ingestion_outbox_events FROM '"+user+"'@'%'")
		rejectedProcess(t, cfg, worker)
		fixtureExec(t, f.db, "GRANT INSERT ON "+shared.Database+".ingestion_outbox_events TO '"+user+"'@'%'")
		fixtureExec(t, f.db, "REVOKE EXECUTE ON FUNCTION "+shared.Database+".data_model_find_record_object_key_v1 FROM '"+user+"'@'%'")
		rejectedProcess(t, cfg, worker)
		fixtureExec(t, f.db, "GRANT EXECUTE ON FUNCTION "+shared.Database+".data_model_find_record_object_key_v1 TO '"+user+"'@'%'")
	}
	fixtureExec(t, f.db, "UPDATE ingestion_schema_migrations SET state='dirty' WHERE step_no=23")
	rejectedProcess(t, cfg, false)
	fixtureExec(t, f.db, "UPDATE ingestion_schema_migrations SET state='applied' WHERE step_no=23")
	fixtureExec(t, f.db, "ALTER TABLE ingestion_outbox_events ADD COLUMN incompatible INT NULL")
	rejectedProcess(t, cfg, false)
	fixtureExec(t, f.db, "ALTER TABLE ingestion_outbox_events DROP COLUMN incompatible")
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "GET", "/readyz", "", "", 200)
}

func TestProcessAPIRuntimeGrants(t *testing.T) {
	shared, f, cfg := processFixture(t)
	_, pool := fullOwner(t, shared, f, "runtime")
	var user string
	if e := pool.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&user); e != nil {
		t.Fatal(e)
	}
	grants, e := PrivateExecutionGrants(shared.Database, user)
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range grants {
		fixtureExec(t, f.db, q)
	}
	cfg["User"] = user
	rejectedProcess(t, cfg, true)
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "POST", recordPath(f), "", `{"object_id":"private","number":1}`, 200)
	expectHTTP(t, api, "GET", readPath(f)+"/private", "", "", 200)
	expectHTTP(t, api, "GET", readPath(f)+"?limit=1", "", "", 200)
	postCSV(t, api, recordPath(f), "object_id,number\nupload,2\n")
}
