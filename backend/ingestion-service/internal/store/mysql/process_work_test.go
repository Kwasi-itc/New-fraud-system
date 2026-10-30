package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func processWork(t *testing.T, f recordFixtureState, cfg map[string]any) *WorkOwner {
	t.Helper()
	owner, e := NewAuthorizedRecordOwner(f.db, f.database, f.model.TenantID, f.model.Tables["records"].ID, "records", processToken, cfg["Limits"].(RecordOwnerLimits), 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	w, e := NewWorkOwner(owner, cfg["UploadQueue"].(string), cfg["DeferredQueue"].(string), 1<<20, cfg["Attempts"].(int))
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func postCSV(t *testing.T, p *testProcess, path, content string, expectedStatus ...int) uuid.UUID {
	t.Helper()
	want := 202
	if len(expectedStatus) > 1 {
		t.Fatal("one expected status required")
	}
	if len(expectedStatus) == 1 {
		want = expectedStatus[0]
	}
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	file, e := m.CreateFormFile("file", "records.csv")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = file.Write([]byte(content)); e != nil {
		t.Fatal(e)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequest("POST", p.address+path+"/csv?mode=patch", &body)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+processToken)
	req.Header.Set("Content-Type", m.FormDataContentType())
	client := &http.Client{Timeout: 10 * time.Second}
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	data, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil || r.StatusCode != want {
		t.Fatal(r.StatusCode, string(data), e)
	}
	if want != 202 {
		return uuid.Nil
	}
	var wire struct {
		UploadLog struct {
			ID string `json:"id"`
		} `json:"upload_log"`
	}
	if e = json.Unmarshal(data, &wire); e != nil {
		t.Fatal(e)
	}
	id, e := uuid.Parse(wire.UploadLog.ID)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestProcessUploadLifecycle(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	id := postCSV(t, api, recordPath(f), "object_id,number\none,1\ntwo,2\n")
	pending := expectHTTP(t, api, "GET", "/v1/upload-logs/"+id.String(), "", "", 200)
	if !bytes.Contains(pending, []byte(`"status":"pending"`)) {
		t.Fatal(string(pending))
	}
	expectHTTP(t, api, "GET", recordPath(f)+"/upload-logs", "", "", 200)
	startProcess(t, cfg, true)
	waitWorkState(t, f.db, id, "completed")
	done := expectHTTP(t, api, "GET", "/v1/upload-logs/"+id.String(), "", "", 200)
	for _, fragment := range []string{`"status":"completed"`, `"total_rows":2`, `"successful_rows":2`, `"attempt_count":1`, `"filename":"records.csv"`} {
		if !bytes.Contains(done, []byte(fragment)) {
			t.Fatal(string(done))
		}
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 2, ExecutionReceipts: 1, Audits: 2, Outbox: 3, Resources: 1, Jobs: 1}) {
		t.Fatal(got)
	}
}
func waitReceiptLock(t *testing.T, f recordFixtureState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := f.db.QueryRow("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='ingestion_receipt_buckets'", f.database).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("request did not reach receipt serialization lock")
}
func TestProcessDeferredLifecycle(t *testing.T) {
	_, f, cfg := processFixture(t)
	cfg["WriteConcurrency"] = 1
	api := startProcess(t, cfg, false)
	key := "hold-sync"
	digest := sha256.Sum256([]byte(key))
	fixtureExec(t, f.db, "INSERT INTO ingestion_receipt_buckets(tenant_id,key_digest) VALUES(?,?)", mysqlstore.UUID(f.model.TenantID), digest[:])
	tx, e := f.db.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	var raw []byte
	if e = tx.QueryRow("SELECT key_digest FROM ingestion_receipt_buckets WHERE tenant_id=? AND key_digest=? FOR UPDATE", mysqlstore.UUID(f.model.TenantID), digest[:]).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		code, body, e := processRequest(ctx, api, "POST", recordPath(f), key, []byte(`{"object_id":"sync","number":1}`))
		if e == nil && code != 200 {
			e = fmt.Errorf("sync status %d %s", code, body)
		}
		done <- e
	}()
	waitReceiptLock(t, f)
	data := expectHTTP(t, api, "PATCH", recordPath(f), "public-deferred", `{"object_id":"deferred","number":2}`, 202)
	var response struct {
		Deferred struct {
			ID string `json:"id"`
		} `json:"deferred_ingest"`
	}
	if e = json.Unmarshal(data, &response); e != nil {
		t.Fatal(e)
	}
	id, e := uuid.Parse(response.Deferred.ID)
	if e != nil {
		t.Fatal(e)
	}
	expectHTTP(t, api, "GET", "/v1/deferred-ingests/"+id.String(), "", "", 200)
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	var retained string
	if e = f.db.QueryRow("SELECT request_key FROM ingestion_work_resources WHERE id=?", mysqlstore.UUID(id)).Scan(&retained); e != nil || retained != "public-deferred" {
		t.Fatal(retained, e)
	}
	startProcess(t, cfg, true)
	waitWorkState(t, f.db, id, "completed")
	data = expectHTTP(t, api, "GET", "/v1/deferred-ingests/"+id.String(), "", "", 200)
	for _, fragment := range []string{`"status":"completed"`, `"attempt_count":1`, `"completed_at":`} {
		if !bytes.Contains(data, []byte(fragment)) {
			t.Fatal(string(data))
		}
	}
	expectHTTP(t, api, "PATCH", recordPath(f), "public-deferred", `{"object_id":"deferred","number":2}`, 200)
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 2, PublicReceipts: 2, Audits: 2, Outbox: 2, Resources: 1, Jobs: 1}) {
		t.Fatal(got)
	}
}
func TestProcessScheduledKinds(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	ctx := WithServiceCredential(context.Background(), processToken)
	var at mysqlstore.DateTime
	if e := f.db.QueryRow("SELECT UTC_TIMESTAMP(6)").Scan(&at); e != nil {
		t.Fatal(e)
	}
	var ids []uuid.UUID
	for _, kind := range []string{UploadWork, DeferredWork} {
		in := workInput(kind)
		in.ScheduledAt = at.Time.Add(6 * time.Second)
		if kind == DeferredWork {
			in.Payload = []byte(`{"object_id":"deferred","number":3}`)
		}
		if _, e := w.Create(ctx, in); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, in.ID)
	}
	startProcess(t, cfg, true)
	for _, id := range ids {
		var state string
		var attempts, maxAttempts int
		var scheduled mysqlstore.DateTime
		if e := f.db.QueryRow("SELECT state,attempt,max_attempts,available_at FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&state, &attempts, &maxAttempts, &scheduled); e != nil || state != "available" || attempts != 0 || maxAttempts != 2 || !scheduled.Time.Equal(at.Time.Add(6*time.Second)) {
			t.Fatal(state, attempts, maxAttempts, scheduled, e)
		}
	}
	for _, id := range ids {
		waitWorkState(t, f.db, id, "completed")
		var early bool
		if e := f.db.QueryRow("SELECT started_at<available_at FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&early); e != nil || early {
			t.Fatal(early, e)
		}
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 3, ExecutionReceipts: 2, Audits: 3, Outbox: 4, Resources: 2, Jobs: 2}) {
		t.Fatal(got)
	}
}
func TestProcessCompetingWorkers(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	ctx := WithServiceCredential(context.Background(), processToken)
	var ids []uuid.UUID
	key := "shared-delivery"
	var now mysqlstore.DateTime
	if e := f.db.QueryRow("SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		in := workInput(DeferredWork)
		in.RequestKey = &key
		in.ScheduledAt = now.Time.Add(8 * time.Second)
		if _, e := w.Create(ctx, in); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, in.ID)
	}
	first := startProcess(t, cfg, true)
	second := startProcess(t, cfg, true)
	for _, id := range ids {
		waitWorkState(t, f.db, id, "completed")
		var attempt int
		if e := f.db.QueryRow("SELECT attempt FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&attempt); e != nil || attempt != 1 {
			t.Fatal(attempt, e)
		}
	}
	first.stop(t)
	second.stop(t)
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1, Resources: 2, Jobs: 2}) {
		t.Fatal(got)
	}
	var active int
	if e := f.db.QueryRow("SELECT COUNT(*) FROM ingestion_work_reservations").Scan(&active); e != nil || active != 0 {
		t.Fatal(active, e)
	}
}

func TestProcessScopedWorkerClaims(t *testing.T) {
	shared, f, cfg := processFixture(t)
	ctx := WithServiceCredential(context.Background(), processToken)
	tenant, table, _ := seedFullContract(t, shared)
	owner, e := NewAuthorizedRecordOwner(f.db, f.database, tenant, table, "transactions", processToken, cfg["Limits"].(RecordOwnerLimits), 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := NewWorkOwner(owner, cfg["UploadQueue"].(string), cfg["DeferredQueue"].(string), 1<<20, 2)
	if e != nil {
		t.Fatal(e)
	}
	a, b := workInput(DeferredWork), workInput(DeferredWork)
	for _, in := range []WorkInput{a, b} {
		if _, e = foreign.Create(ctx, in); e != nil {
			t.Fatal(e)
		}
	}
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='running',attempt=1,lease_token=UUID_TO_BIN(UUID()),lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(b.ID))
	own := processWork(t, f, cfg)
	unselected := workInput(DeferredWork)
	if _, e = own.Create(ctx, unselected); e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET queue='unselected',state='running',attempt=1,lease_token=UUID_TO_BIN(UUID()),lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(unselected.ID))
	process := startProcess(t, cfg, true)
	time.Sleep(400 * time.Millisecond)
	process.stop(t)
	for id, want := range map[uuid.UUID]string{a.ID: "available", b.ID: "running", unselected.ID: "running"} {
		var state string
		if e = f.db.QueryRow("SELECT state FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&state); e != nil || state != want {
			t.Fatal(id, state, e)
		}
	}
}

func TestProcessFailedWorkDetails(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	w := processWork(t, f, cfg)
	ctx := WithServiceCredential(context.Background(), processToken)
	for _, kind := range []string{UploadWork, DeferredWork} {
		in := workInput(kind)
		if _, e := w.Create(ctx, in); e != nil {
			t.Fatal(e)
		}
		fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='discarded',attempt=2,started_at=UTC_TIMESTAMP(6),finished_at=UTC_TIMESTAMP(6),last_error='handler_failed' WHERE id=?", mysqlstore.UUID(in.ID))
		fixtureExec(t, f.db, "DELETE FROM ingestion_work_reservations WHERE resource_id=?", mysqlstore.UUID(in.ID))
		route := "/v1/upload-logs/"
		if kind == DeferredWork {
			route = "/v1/deferred-ingests/"
		}
		data := expectHTTP(t, api, "GET", route+in.ID.String(), "", "", 200)
		values := []string{`"status":"failed"`, `"attempt_count":2`, `handler_failed`}
		// Upload's established DTO has no timestamps; deferred's DTO does.
		if kind == DeferredWork {
			values = append(values, `started_at`, `completed_at`, `requested_at`)
		}
		detail, e := w.Detail(ctx, in.ID, kind)
		if e != nil || detail.RequestedAt.IsZero() || detail.StartedAt == nil || detail.CompletedAt == nil {
			t.Fatal(detail, e)
		}
		for _, value := range values {
			if !bytes.Contains(data, []byte(value)) {
				t.Fatal(string(data))
			}
		}
	}
}
