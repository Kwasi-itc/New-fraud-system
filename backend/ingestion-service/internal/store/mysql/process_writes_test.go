package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func concurrentRequests(t *testing.T, p *testProcess, path string, keys, bodies []string) []int {
	t.Helper()
	start := make(chan struct{})
	codes := make([]int, len(keys))
	errs := make([]error, len(keys))
	data := make([][]byte, len(keys))
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			codes[i], data[i], errs[i] = processRequest(ctx, p, "POST", path, keys[i], []byte(bodies[i]))
		}(i)
	}
	close(start)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatal(i, e, string(data[i]))
		}
	}
	return codes
}
func TestProcessIdenticalReceipts(t *testing.T) {
	_, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	keys, bodies := make([]string, 12), make([]string, 12)
	for i := range keys {
		keys[i] = "one-command"
		bodies[i] = `{"object_id":"one","number":1}`
	}
	for _, code := range concurrentRequests(t, p, recordPath(f), keys, bodies) {
		if code != 200 {
			t.Fatal(code)
		}
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1}) {
		t.Fatal(got)
	}
}
func TestProcessConflictingReceipts(t *testing.T) {
	_, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	path := recordPath(f)
	codes := concurrentRequests(t, p, path, []string{"same", "same"}, []string{`{"object_id":"one","number":1}`, `{"object_id":"two","number":2}`})
	if codes[0]+codes[1] != 609 {
		t.Fatal(codes)
	}
	expectHTTP(t, p, "PATCH", path, "same", `{"object_id":"one","number":1}`, 409)
	expectHTTP(t, p, "POST", path+"/batch", "same", `[{"object_id":"one","number":1}]`, 409)
	expectHTTP(t, p, "POST", strings.Replace(path, "/records", "/foreign", 1), "same", `{"object_id":"one"}`, 403)
	expectHTTP(t, p, "POST", strings.Replace(path, f.model.TenantID.String(), uuid.NewString(), 1), "same", `{"object_id":"one"}`, 403)
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1}) {
		t.Fatal(got)
	}
}
func TestProcessAlternateUniqueRace(t *testing.T) {
	_, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	path := recordPath(f)
	codes := concurrentRequests(t, p, path, []string{"first", "second"}, []string{`{"object_id":"one","number":1}`, `{"object_id":"two","number":1}`})
	if codes[0]+codes[1] != 609 {
		t.Fatal(codes)
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1}) {
		t.Fatal(got)
	}
	// A late secondary collision rolls the entire ordered batch back.
	expectHTTP(t, p, "POST", path+"/batch", "late", `[{"object_id":"new","number":2},{"object_id":"collision","number":1}]`, 409)
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1}) {
		t.Fatal(got)
	}
}
func TestProcessEffectGrantRollback(t *testing.T) {
	shared, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	user := cfg["User"].(string)
	path := recordPath(f)
	for i, table := range []string{"ingestion_audit", "ingestion_outbox_events", "ingestion_request_receipts", "ingestion_execution_receipts"} {
		fixtureExec(t, f.db, "REVOKE INSERT ON "+shared.Database+"."+table+" FROM '"+user+"'@'%'")
		key := fmt.Sprintf("grant-%d", i)
		if table == "ingestion_execution_receipts" {
			key = ""
		}
		expectHTTP(t, p, "PATCH", path, key, fmt.Sprintf(`{"object_id":"grant-%d"}`, i), 503)
		if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{}) {
			t.Fatal(table, got)
		}
		fixtureExec(t, f.db, "GRANT INSERT ON "+shared.Database+"."+table+" TO '"+user+"'@'%'")
	}
	expectHTTP(t, p, "PATCH", path, "grant-0", `{"object_id":"grant-0"}`, 200)
}
func TestProcessCancellationBeforeCommit(t *testing.T) {
	_, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	fixtureExec(t, f.db, "CREATE TRIGGER pause_outbox BEFORE INSERT ON ingestion_outbox_events FOR EACH ROW DO SLEEP(2)")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	code, _, err := processRequest(ctx, p, "POST", recordPath(f), "cancel", []byte(`{"object_id":"cancelled","number":1}`))
	if err == nil {
		t.Fatal(code, "expected client cancellation")
	}
	// Let the isolated server observe the disconnected query and release its locks.
	time.Sleep(2200 * time.Millisecond)
	fixtureExec(t, f.db, "DROP TRIGGER pause_outbox")
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{}) {
		t.Fatal(got)
	}
	expectHTTP(t, p, "POST", recordPath(f), "cancel", `{"object_id":"cancelled","number":1}`, 200)
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1}) {
		t.Fatal(got)
	}
}
func TestProcessOrderedPatches(t *testing.T) {
	_, f, cfg := processFixture(t)
	p := startProcess(t, cfg, false)
	path := recordPath(f)
	expectHTTP(t, p, "POST", path+"/batch", "create", `[{"object_id":"one","number":1,"amount":1.5},{"object_id":"two","number":2,"flag":true}]`, 200)
	data := expectHTTP(t, p, "PATCH", path+"/batch", "patch", `[{"object_id":"two","amount":2.5},{"object_id":"one","flag":false}]`, 200)
	var result struct {
		Results []struct{ ObjectID, Action string }
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	// Decode the public snake-case DTO explicitly.
	var wire struct{ Results []map[string]any }
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Results) != 2 || wire.Results[0]["object_id"] != "two" || wire.Results[1]["object_id"] != "one" {
		t.Fatal(string(data))
	}
	q := "SELECT `" + f.columns["number"]["value"] + "`,`" + f.columns["amount"]["value"] + "`,`" + f.columns["flag"]["value"] + "` FROM " + f.qualified + " WHERE `" + f.columns["object_id"]["value"] + "`=?"
	var number int
	var amount float64
	var flag bool
	if err := f.db.QueryRow(q, "one").Scan(&number, &amount, &flag); err != nil || number != 1 || amount != 1.5 || flag {
		t.Fatal(number, amount, flag, err)
	}
	if err := f.db.QueryRow(q, "two").Scan(&number, &amount, &flag); err != nil || number != 2 || amount != 2.5 || !flag {
		t.Fatal(number, amount, flag, err)
	}
	expectHTTP(t, p, "PATCH", path+"/batch", "duplicate", `[{"object_id":"one","amount":9},{"object_id":"one","amount":10}]`, 422)
	var auditCount int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM ingestion_outbox_events WHERE tenant_id=?", mysqlstore.UUID(f.model.TenantID)).Scan(&auditCount); err != nil || auditCount != 6 {
		t.Fatal(auditCount, err)
	}
}

func TestProcessLimitsAndAuthentication(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	records := make([]map[string]any, 501)
	for i := range records {
		records[i] = map[string]any{"object_id": fmt.Sprintf("limit-%d", i), "number": i}
	}
	wire, e := json.Marshal(records)
	if e != nil {
		t.Fatal(e)
	}
	expectHTTP(t, api, "POST", recordPath(f)+"/batch", "", string(wire), 400)
	data := expectHTTP(t, api, "POST", recordPath(f), strings.Repeat("k", 2048), `{"object_id":"limit","number":1}`, 413)
	if !strings.Contains(string(data), `"code":"request_limit"`) {
		t.Fatal(string(data))
	}
	expectHTTP(t, api, "POST", recordPath(f), "", strings.Repeat(" ", (1<<20)+(64<<10)+1), 413)
	for _, token := range []string{"", "invalid-token"} {
		req, e := http.NewRequest("POST", api.address+recordPath(f), strings.NewReader(`{"object_id":"unauthorized"}`))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatal(response.StatusCode)
		}
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{}) {
		t.Fatal(got)
	}
}
