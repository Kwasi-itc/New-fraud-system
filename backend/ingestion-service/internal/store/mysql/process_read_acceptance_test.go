package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func readPath(f recordFixtureState) string {
	return "/v1/tenants/" + f.model.TenantID.String() + "/records/records"
}
func TestProcessReadContracts(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	for i, id := range []string{"b", "a", "a_%", "A", "aX"} {
		expectHTTP(t, api, "POST", recordPath(f), id, fmt.Sprintf(`{"object_id":%q,"number":%d,"amount":2.5,"flag":true}`, id, i+1), 200)
	}
	fixtureExec(t, f.db, "UPDATE "+f.qualified+" SET `"+f.columns["updated_at"]["value"]+"`='2026-10-30 00:00:00.000000'")
	data := expectHTTP(t, api, "GET", readPath(f)+"?limit=2", "", "", 200)
	var page struct {
		Records []struct {
			ObjectID string `json:"object_id"`
		}
	}
	if e := json.Unmarshal(data, &page); e != nil || len(page.Records) != 2 || page.Records[0].ObjectID != "A" || page.Records[1].ObjectID != "a" {
		t.Fatal(string(data), e)
	}
	expectHTTP(t, api, "GET", readPath(f)+"/missing", "", "", 404)
	data = expectHTTP(t, api, "GET", readPath(f)+"/a", "", "", 200)
	if !strings.Contains(string(data), `"number":2`) {
		t.Fatal(string(data))
	}
	data = expectHTTP(t, api, "GET", readPath(f)+"/search?field=number&value=3", "", "", 200)
	if !strings.Contains(string(data), `"object_id":"a_%"`) {
		t.Fatal(string(data))
	}
	expectHTTP(t, api, "GET", readPath(f)+"?limit=1001", "", "", 413)
	expectHTTP(t, api, "GET", strings.Replace(readPath(f), f.model.TenantID.String(), uuid.NewString(), 1), "", "", 403)
	path := "/v1/tenants/" + f.model.TenantID.String() + "/query/aggregate"
	for op, want := range map[string]string{"count": "5", "sum": "15", "avg": "3", "min": "1", "max": "5", "count_distinct": "5"} {
		data = expectHTTP(t, api, "POST", path, "", fmt.Sprintf(`{"object_type":"records","aggregate":%q,"field":"number"}`, op), 200)
		var out struct{ Value json.Number }
		if e := json.Unmarshal(data, &out); e != nil {
			t.Fatal(string(data), e)
		}
		n, e := out.Value.Float64()
		if e != nil || fmt.Sprint(n) != want {
			t.Fatal(op, string(data), e)
		}
	}
	data = expectHTTP(t, api, "POST", path, "", `{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"predicate","field":"object_id","op":"starts_with","value":"a_%"}}`, 200)
	if string(data) != `{"value":1}` {
		t.Fatal(string(data))
	}
	expectHTTP(t, api, "POST", path, "", `{"object_type":"foreign","aggregate":"count","field":"number"}`, 403)
	// Writes and exact reads share current publication and typed projection.
	for i := 0; i < 5; i++ {
		expectHTTP(t, api, "PATCH", recordPath(f), fmt.Sprintf("update-%d", i), fmt.Sprintf(`{"object_id":"a","amount":%d}`, i), 200)
		data = expectHTTP(t, api, "GET", readPath(f)+"/a", "", "", 200)
		if !strings.Contains(string(data), fmt.Sprintf(`"amount":%d`, i)) {
			t.Fatal(string(data))
		}
	}
}
func TestProcessReadRevocation(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "POST", recordPath(f), "original", `{"object_id":"one","number":1}`, 200)
	fixtureExec(t, f.db, "UPDATE data_model_tenants SET status='pending' WHERE id=?", mysqlstore.UUID(f.model.TenantID))
	expectHTTP(t, api, "GET", readPath(f)+"/one", "", "", 403)
	expectHTTP(t, api, "POST", recordPath(f), "original", `{"object_id":"one","number":1}`, 403)
	fixtureExec(t, f.db, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(f.model.TenantID))
	expectHTTP(t, api, "GET", readPath(f)+"/one", "", "", 200)
}

func TestProcessPublicationRace(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	table := mysqlstore.UUID(f.model.Tables["records"].ID)
	tx, e := f.db.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE data_model_physical_tables SET write_state='closed' WHERE table_id=?", table); e != nil {
		t.Fatal(e)
	}
	done := make(chan int, 1)
	go func() {
		code, _, _ := processRequest(context.Background(), api, "POST", recordPath(f), "publication-race", []byte(`{"object_id":"blocked","number":1}`))
		done <- code
	}()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var n int
		e = f.db.QueryRow("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='data_model_physical_tables'", f.database).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("request did not wait for publication")
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if code := <-done; code != 503 {
		t.Fatal(code)
	}
	if got := fullDurableCounts(t, f); got.Records != 0 || got.Outbox != 0 || got.PublicReceipts != 0 {
		t.Fatal(got)
	}
	fixtureExec(t, f.db, "UPDATE data_model_physical_tables SET write_state='open' WHERE table_id=?", table)
	expectHTTP(t, api, "POST", recordPath(f), "publication-race", `{"object_id":"blocked","number":1}`, 200)
}

func TestProcessConcurrentReadsAndWrites(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "POST", recordPath(f), "seed", `{"object_id":"one","number":1,"amount":0}`, 200)
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 12; i++ {
			code, _, e := processRequest(context.Background(), api, "PATCH", recordPath(f), fmt.Sprintf("revision-%d", i), []byte(fmt.Sprintf(`{"object_id":"one","amount":%d}`, i)))
			if e != nil || code != 200 {
				done <- fmt.Errorf("write %d: %d %v", i, code, e)
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 12; i++ {
		data := expectHTTP(t, api, "GET", readPath(f)+"/one", "", "", 200)
		var result struct {
			Record struct{ Fields map[string]any }
		}
		if e := json.Unmarshal(data, &result); e != nil {
			t.Fatal(e)
		}
		if result.Record.Fields["number"] != float64(1) {
			t.Fatal(string(data))
		}
		amount, ok := result.Record.Fields["amount"].(float64)
		if !ok || amount < 0 || amount > 12 {
			t.Fatal(string(data))
		}
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}

func TestProcessRevocationDuringReplay(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	body := `{"object_id":"one","number":1}`
	expectHTTP(t, api, "POST", recordPath(f), "revocation-race", body, 200)
	tx := blockPublicKey(t, f, "revocation-race")
	done := make(chan int, 1)
	go func() {
		code, _, _ := processRequest(context.Background(), api, "POST", recordPath(f), "revocation-race", []byte(body))
		done <- code
	}()
	waitReceiptLock(t, f)
	fixtureExec(t, f.db, "UPDATE data_model_tables SET archived=1 WHERE id=?", mysqlstore.UUID(f.model.Tables["records"].ID))
	if e := tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	if code := <-done; code != 403 {
		t.Fatal("stale replay authorization", code)
	}
	fixtureExec(t, f.db, "UPDATE data_model_tables SET archived=0 WHERE id=?", mysqlstore.UUID(f.model.Tables["records"].ID))
	expectHTTP(t, api, "POST", recordPath(f), "revocation-race", body, 200)
	if got := fullDurableCounts(t, f); got.Records != 1 || got.PublicReceipts != 1 || got.Audits != 1 || got.Outbox != 1 {
		t.Fatal(got)
	}
}
