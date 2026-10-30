package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

func blockPublicKey(t *testing.T, f recordFixtureState, key string) *sql.Tx {
	t.Helper()
	digest := sha256.Sum256([]byte(key))
	fixtureExec(t, f.db, "INSERT INTO ingestion_receipt_buckets(tenant_id,key_digest) VALUES(?,?) ON DUPLICATE KEY UPDATE key_digest=key_digest", mysqlstore.UUID(f.model.TenantID), digest[:])
	tx, e := f.db.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { tx.Rollback() })
	var n int
	if e = tx.QueryRow("SELECT 1 FROM ingestion_receipt_buckets WHERE tenant_id=? AND key_digest=? FOR UPDATE", mysqlstore.UUID(f.model.TenantID), digest[:]).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return tx
}
func TestProcessWorkerInterruption(t *testing.T) {
	for _, kill := range []bool{false, true} {
		t.Run(map[bool]string{false: "shutdown", true: "kill"}[kill], func(t *testing.T) {
			_, f, cfg := processFixture(t)
			w := processWork(t, f, cfg)
			key := "blocked-work"
			tx := blockPublicKey(t, f, key)
			in := workInput(DeferredWork)
			in.RequestKey = &key
			ctx := WithServiceCredential(context.Background(), processToken)
			if _, e := w.Create(ctx, in); e != nil {
				t.Fatal(e)
			}
			worker := startProcess(t, cfg, true)
			waitWorkState(t, f.db, in.ID, "running")
			waitReceiptLock(t, f)
			var token mysqlstore.UUID
			var attempt int
			if e := f.db.QueryRow("SELECT lease_token,attempt FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(in.ID)).Scan(&token, &attempt); e != nil {
				t.Fatal(e)
			}
			old := work.Lease{Job: work.Job{ID: in.ID, ResourceID: in.ID, TenantID: f.model.TenantID, Kind: DeferredWork, Version: 1}, Token: uuid.UUID(token), Attempt: attempt}
			start := time.Now()
			if kill {
				if e := worker.cmd.Process.Kill(); e != nil {
					t.Fatal(e)
				}
				<-worker.done
				worker.err = nil
			} else {
				worker.stop(t)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("shutdown exceeded configured budget")
			}
			if e := tx.Rollback(); e != nil {
				t.Fatal(e)
			}
			if got := fullDurableCounts(t, f); got.Records != 0 || got.Audits != 0 || got.Outbox != 0 {
				t.Fatal(got)
			}
			startProcess(t, cfg, true)
			waitWorkState(t, f.db, in.ID, "completed")
			if e := w.Handle(ctx, old); !errors.Is(e, work.ErrLease) {
				t.Fatal("stale lease accepted", e)
			}
			got := fullDurableCounts(t, f)
			if got.Records != 1 || got.PublicReceipts != 1 || got.Audits != 1 || got.Outbox != 1 {
				t.Fatal(got)
			}
		})
	}
}
func TestProcessWorkerCommitLoss(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	proxy := startCommitProxy(t)
	proxy.finalOnly.Store(true)
	cfg["Address"] = proxy.address
	worker := startProcess(t, cfg, true)
	proxy.arm.Store(true)
	in := workInput(DeferredWork)
	ctx := WithServiceCredential(context.Background(), processToken)
	if _, e := w.Create(ctx, in); e != nil {
		t.Fatal(e)
	}
	waitWorkState(t, f.db, in.ID, "completed")
	deadline := time.Now().Add(time.Second)
	for !proxy.observed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !proxy.observed.Load() {
		t.Fatal("final COMMIT OK not intercepted")
	}
	worker.stop(t)
	startProcess(t, cfg, true)
	time.Sleep(100 * time.Millisecond)
	got := fullDurableCounts(t, f)
	if got.Records != 1 || got.ExecutionReceipts != 1 || got.Audits != 1 || got.Outbox != 1 {
		t.Fatal(got)
	}
	var attempt int
	if e := f.db.QueryRow("SELECT attempt FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(in.ID)).Scan(&attempt); e != nil || attempt != 1 {
		t.Fatal(attempt, e)
	}
}
func TestProcessMalformedWork(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	ctx := WithServiceCredential(context.Background(), processToken)
	var ids []uuid.UUID
	for _, item := range []struct{ kind, payload string }{{UploadWork, "object_id,object_id\na,b\n"}, {DeferredWork, "{"}, {DeferredWork, `{"object_id":"one"} {}`}, {DeferredWork, `{"object_id":"invalid","number":"wrong"}`}} {
		in := workInput(item.kind)
		in.Payload = []byte(item.payload)
		malformed := item.kind == DeferredWork && !json.Valid(in.Payload)
		if malformed {
			if _, e := w.Create(ctx, in); e == nil {
				t.Fatal("malformed submission accepted")
			}
			in.Payload = []byte(`{"object_id":"corrupt-fixture"}`)
		}
		if _, e := w.Create(ctx, in); e != nil {
			t.Fatal(e)
		}
		if malformed {
			fixtureExec(t, f.db, "UPDATE ingestion_work_resources SET payload=? WHERE id=?", []byte(item.payload), mysqlstore.UUID(in.ID))
		}
		ids = append(ids, in.ID)
	}
	startProcess(t, cfg, true)
	for i, id := range ids {
		waitWorkState(t, f.db, id, "discarded")
		detail, e := w.Detail(ctx, id, map[bool]string{true: UploadWork, false: DeferredWork}[i == 0])
		if e != nil || detail.Attempt != 1 || detail.ErrorCode == nil {
			t.Fatal(detail, e)
		}
		want := "invalid_payload"
		if i == 3 {
			want = "validation_failed"
			if detail.TotalRows != 1 || detail.FailedRows != 1 {
				t.Fatal(detail)
			}
		}
		if *detail.ErrorCode != want {
			t.Fatal(detail)
		}
	}
	got := fullDurableCounts(t, f)
	if got.Records != 0 || got.Outbox != 0 || got.ExecutionReceipts != 0 || got.Audits != 1 {
		t.Fatal(got)
	}
}
func TestProcessRetryExhaustion(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	ctx := WithServiceCredential(context.Background(), processToken)
	fixtureExec(t, f.db, "CREATE TRIGGER reject_outbox BEFORE INSERT ON ingestion_outbox_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated failure'")
	in := workInput(DeferredWork)
	if _, e := w.Create(ctx, in); e != nil {
		t.Fatal(e)
	}
	startProcess(t, cfg, true)
	waitWorkState(t, f.db, in.ID, "discarded")
	detail, e := w.Detail(ctx, in.ID, DeferredWork)
	if e != nil || detail.Attempt != 2 || detail.CompletedAt == nil || detail.ErrorCode == nil || *detail.ErrorCode != "handler_failed" {
		t.Fatal(detail, e)
	}
	got := fullDurableCounts(t, f)
	if got.Records != 0 || got.ExecutionReceipts != 0 || got.Audits != 0 || got.Outbox != 0 {
		t.Fatal(got)
	}
	var reservations int
	if e = f.db.QueryRow("SELECT COUNT(*) FROM ingestion_work_reservations").Scan(&reservations); e != nil || reservations != 0 {
		t.Fatal(reservations, e)
	}
}
func TestProcessDependencyOutage(t *testing.T) {
	_, f, cfg := processFixture(t)
	proxy := startCommitProxy(t)
	cfg["Address"] = proxy.address
	api := startProcess(t, cfg, false)
	worker := startProcess(t, cfg, true)
	proxy.offline.Store(true)
	proxy.disconnect()
	expectHTTP(t, api, "POST", recordPath(f), "outage", `{"object_id":"one","number":1}`, 503)
	w := processWork(t, f, cfg)
	in := workInput(DeferredWork)
	in.Payload = []byte(`{"object_id":"two","number":2}`)
	ctx := WithServiceCredential(context.Background(), processToken)
	if _, e := w.Create(ctx, in); e != nil {
		t.Fatal(e)
	}
	time.Sleep(150 * time.Millisecond)
	if got := fullDurableCounts(t, f); got.Records != 0 || got.Outbox != 0 {
		t.Fatal(got)
	}
	proxy.offline.Store(false)
	expectHTTP(t, api, "POST", recordPath(f), "outage", `{"object_id":"one","number":1}`, 200)
	waitWorkState(t, f.db, in.ID, "completed")
	worker.stop(t)
	if got := fullDurableCounts(t, f); got.Records != 2 || got.Audits != 2 || got.Outbox != 2 {
		t.Fatal(got)
	}
}
func TestProcessOperatorRecovery(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	ctx := WithServiceCredential(context.Background(), processToken)
	operator, e := NewWorkOperator(w, func(ctx context.Context, tx *sql.Tx, scope WorkOperatorScope) (string, error) {
		if scope.TenantID != f.model.TenantID || scope.TableID != f.model.Tables["records"].ID {
			return "", ErrOwnerAuthorization
		}
		return "isolated-trusted-operator", nil
	})
	if e != nil {
		t.Fatal(e)
	}
	cancelled := workInput(DeferredWork)
	if _, e = w.Create(ctx, cancelled); e != nil {
		t.Fatal(e)
	}
	command := CancelWorkRequest{ResourceID: cancelled.ID, ActionID: uuid.New(), ReasonCode: "operator_request"}
	if _, e = operator.Cancel(ctx, command); e != nil {
		t.Fatal(e)
	}
	if _, e = operator.Cancel(ctx, command); e != nil {
		t.Fatal(e)
	}
	in := workInput(DeferredWork)
	if _, e = w.Create(ctx, in); e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='discarded',last_error='handler_failed',finished_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(in.ID))
	fixtureExec(t, f.db, "DELETE FROM ingestion_work_reservations WHERE resource_id=?", mysqlstore.UUID(in.ID))
	action := RedriveWorkRequest{ResourceID: in.ID, PreviousJobID: in.ID, ActionID: uuid.New(), ReasonCode: "dependency_recovered", ScheduledAt: time.Now().UTC().Truncate(time.Microsecond)}
	id, e := operator.Redrive(ctx, action)
	if e != nil {
		t.Fatal(e)
	}
	again, e := operator.Redrive(ctx, action)
	if e != nil || again != id {
		t.Fatal(again, e)
	}
	startProcess(t, cfg, true)
	waitWorkState(t, f.db, id, "completed")
	var state string
	if e = f.db.QueryRow("SELECT state FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(cancelled.ID)).Scan(&state); e != nil || state != "cancelled" {
		t.Fatal(state, e)
	}
	if got := fullDurableCounts(t, f); got.Records != 1 || got.ExecutionReceipts != 1 || got.Audits != 1 || got.Outbox != 1 {
		t.Fatal(got)
	}
}

func TestProcessHandlerTimeout(t *testing.T) {
	_, f, cfg := processFixture(t)
	w := processWork(t, f, cfg)
	key := "timeout-key"
	tx := blockPublicKey(t, f, key)
	in := workInput(DeferredWork)
	in.RequestKey = &key
	ctx := WithServiceCredential(context.Background(), processToken)
	if _, e := w.Create(ctx, in); e != nil {
		t.Fatal(e)
	}
	startProcess(t, cfg, true)
	waitWorkState(t, f.db, in.ID, "running")
	// The final transaction holds the job fence while waiting for its receipt.
	// Heartbeat cannot acquire that row; bounded handler timeout must roll back.
	waitWorkState(t, f.db, in.ID, "discarded")
	if e := tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	d, e := w.Detail(ctx, in.ID, DeferredWork)
	if e != nil || d.Attempt != 2 {
		t.Fatal(d, e)
	}
	if got := fullDurableCounts(t, f); got.Records != 0 || got.Outbox != 0 {
		t.Fatal(got)
	}
}
