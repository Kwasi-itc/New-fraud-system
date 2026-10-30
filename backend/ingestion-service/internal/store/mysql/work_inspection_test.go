package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func inspectionFixture(t *testing.T) (recordFixtureState, *WorkOwner, *WorkInspector) {
	t.Helper()
	f, w := workFixture(t)
	ddl, err := WorkInspectionIndex(f.database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	r, err := NewWorkInspector(f.db, f.database, w.owner.receipts.tenant, func(_ context.Context, _ *sql.Tx, s QueueInspectionScope) error {
		if s.TenantID != w.owner.receipts.tenant || s.Queue != "uploads" {
			return ErrOwnerAuthorization
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, w, r
}

func TestWorkInspectionBoundsAndAuthorization(t *testing.T) {
	f, w, r := inspectionFixture(t)
	seedUploadList(t, f, w, 4)
	ctx := context.Background()
	good := WorkInspectionRequest{Queue: "uploads", State: "completed", Limit: 1}
	if _, err := NewWorkInspector(f.db, f.database, r.tenant, nil); err == nil {
		t.Fatal("missing policy")
	}
	for _, in := range []WorkInspectionRequest{{Queue: "uploads", State: "completed", Limit: 0}, {Queue: "uploads", State: "completed", Limit: 101}, {Queue: "*", State: "completed", Limit: 1}, {Queue: "uploads", State: "*", Limit: 1}} {
		if p, err := r.List(ctx, in); err == nil || len(p.Items) != 0 {
			t.Fatal(p, err)
		}
	}
	first, err := r.List(ctx, good)
	if err != nil || first.Next == nil {
		t.Fatal(first, err)
	}
	for _, change := range []func(*WorkInspectionCursor){func(c *WorkInspectionCursor) { c.TenantID = uuid.New() }, func(c *WorkInspectionCursor) { c.Queue = "other" }, func(c *WorkInspectionCursor) { c.State = "running" }, func(c *WorkInspectionCursor) { c.CreatedAt = time.Time{} }, func(c *WorkInspectionCursor) { c.ID = uuid.Nil }} {
		c := *first.Next
		change(&c)
		in := good
		in.Cursor = &c
		if p, err := r.List(ctx, in); err == nil || len(p.Items) != 0 {
			t.Fatal(p, err)
		}
	}
	r.authorize = func(context.Context, *sql.Tx, QueueInspectionScope) error { return ErrOwnerAuthorization }
	good.Cursor = first.Next
	if p, err := r.List(ctx, good); !errors.Is(err, ErrOwnerAuthorization) || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
	r.authorize = func(context.Context, *sql.Tx, QueueInspectionScope) error { return nil }
	r.tenant = uuid.New()
	good.Cursor = nil
	if p, err := r.List(ctx, good); err != nil || len(p.Items) != 0 {
		t.Fatal("foreign tenant rows", p, err)
	}
}

func TestWorkInspectionPaginationAndPlan(t *testing.T) {
	f, w, r := inspectionFixture(t)
	seedUploadList(t, f, w, 2000)
	// Split history across queues and terminal states while retaining valid rows.
	if _, err := f.db.Exec("UPDATE "+r.schema+".ingestion_work_jobs SET queue='other' WHERE kind=?", DeferredWork); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE " + r.schema + ".ingestion_work_jobs SET state='discarded' WHERE queue='uploads' LIMIT 200"); err != nil {
		t.Fatal(err)
	}
	in := WorkInspectionRequest{Queue: "uploads", State: "completed", Limit: 37}
	seen := map[uuid.UUID]bool{}
	var previous *WorkInspectionItem
	for {
		page, err := r.List(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] || item.Kind != UploadWork || item.Attempt != 1 || item.StartedAt == nil || item.FinishedAt == nil {
				t.Fatal(item)
			}
			if previous != nil && (item.CreatedAt.After(previous.CreatedAt) || (item.CreatedAt.Equal(previous.CreatedAt) && strings.Compare(item.ID.String(), previous.ID.String()) >= 0)) {
				t.Fatal("unstable order")
			}
			seen[item.ID] = true
			copy := item
			previous = &copy
		}
		if page.Next == nil {
			break
		}
		in.Cursor = page.Next
	}
	if len(seen) != 800 {
		t.Fatal("filter/pagination count", len(seen))
	}
	if _, err := f.db.Exec("ANALYZE TABLE " + r.schema + ".ingestion_work_jobs"); err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []*WorkInspectionCursor{nil, in.Cursor} {
		in.Cursor = cursor
		q, args := r.inspectionQuery(in)
		var raw string
		if err := f.db.QueryRow("EXPLAIN FORMAT=JSON "+q, args...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		t.Log("inspection plan:", raw)
		var plan any
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			t.Fatal(err)
		}
		indexed := false
		filesort := false
		var visit func(any)
		visit = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if v["key"] == "operator_history" && (v["access_type"] == "ref" || v["access_type"] == "range") {
					indexed = true
				}
				if v["using_filesort"] == true {
					filesort = true
				}
				for _, child := range v {
					visit(child)
				}
			case []any:
				for _, child := range v {
					visit(child)
				}
			}
		}
		visit(plan)
		if !indexed || filesort {
			t.Fatal("expected scoped ordered index", raw)
		}
	}
}

func TestWorkInspectionRestrictedGrants(t *testing.T) {
	f, w, r := inspectionFixture(t)
	seedUploadList(t, f, w, 4)
	pool := restrictedReceiptPool(t, f.db, f.database)
	var login string
	if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	account := strings.Split(login, "@")[0]
	if _, err := f.db.Exec("REVOKE ALL PRIVILEGES, GRANT OPTION FROM '" + account + "'@'%'"); err != nil {
		t.Fatal(err)
	}
	grants, err := InspectionGrants(f.database, account)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		if _, err = f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	r.db = pool
	if p, err := r.List(context.Background(), WorkInspectionRequest{Queue: "uploads", State: "completed", Limit: 10}); err != nil || len(p.Items) != 4 {
		t.Fatal(p, err)
	}
	for _, q := range []string{"SELECT payload FROM " + r.schema + ".ingestion_work_jobs", "SELECT lease_token FROM " + r.schema + ".ingestion_work_jobs", "SELECT last_error FROM " + r.schema + ".ingestion_work_jobs", "SELECT payload FROM " + r.schema + ".ingestion_work_resources", "UPDATE " + r.schema + ".ingestion_work_jobs SET attempt=0", "DELETE FROM " + r.schema + ".ingestion_work_jobs", "SELECT response_payload FROM " + r.schema + ".ingestion_request_receipts"} {
		rows, err := pool.Query(q)
		if err == nil {
			rows.Close()
			t.Fatal("forbidden", q)
		}
	}
}

func TestWorkInspectionConcurrentReadsAndFailure(t *testing.T) {
	f, w, r := inspectionFixture(t)
	seedUploadList(t, f, w, 10)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := r.List(context.Background(), WorkInspectionRequest{Queue: "uploads", State: "completed", Limit: 10})
			if err != nil || len(p.Items) != 10 {
				t.Error(p, err)
			}
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, err := r.List(ctx, WorkInspectionRequest{Queue: "uploads", State: "completed", Limit: 10}); err == nil || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
	// A policy may read metadata, but cannot mutate it through this read-only tx.
	r.authorize = func(ctx context.Context, tx *sql.Tx, _ QueueInspectionScope) error {
		_, err := tx.ExecContext(ctx, "UPDATE "+r.schema+".ingestion_work_jobs SET attempt=0")
		return err
	}
	if p, err := r.List(context.Background(), WorkInspectionRequest{Queue: "uploads", State: "completed", Limit: 10}); err == nil || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
}

func TestWorkInspectionStateFilters(t *testing.T) {
	f, w, r := inspectionFixture(t)
	seedUploadList(t, f, w, 10)
	rows, err := f.db.Query("SELECT id FROM " + r.schema + ".ingestion_work_jobs ORDER BY id LIMIT 5")
	if err != nil {
		t.Fatal(err)
	}
	var ids []mysqlstore.UUID
	for rows.Next() {
		var id mysqlstore.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for i, state := range []string{"available", "running", "completed", "discarded", "cancelled"} {
		query := "UPDATE " + r.schema + ".ingestion_work_jobs SET queue='states',state=?,finished_at=NULL WHERE id=?"
		if state == "running" {
			query = "UPDATE " + r.schema + ".ingestion_work_jobs SET queue='states',state=?,finished_at=NULL,lease_token=UUID_TO_BIN(UUID()),lease_until=TIMESTAMPADD(MINUTE,5,UTC_TIMESTAMP(6)) WHERE id=?"
		}
		if state == "completed" || state == "discarded" || state == "cancelled" {
			query = "UPDATE " + r.schema + ".ingestion_work_jobs SET queue='states',state=? WHERE id=?"
		}
		if _, err := f.db.Exec(query, state, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	r.authorize = func(_ context.Context, _ *sql.Tx, s QueueInspectionScope) error {
		if s.TenantID != r.tenant || s.Queue != "states" {
			return ErrOwnerAuthorization
		}
		return nil
	}
	for i, state := range []string{"available", "running", "completed", "discarded", "cancelled"} {
		p, err := r.List(context.Background(), WorkInspectionRequest{Queue: "states", State: state, Limit: 10})
		if err != nil || len(p.Items) != 1 || p.Items[0].ID != uuid.UUID(ids[i]) {
			t.Fatal(state, p, err)
		}
		if (p.Items[0].FinishedAt == nil) != (state == "available" || state == "running") {
			t.Fatal("terminal timestamp", state)
		}
	}
	// Readiness must not silently degrade to scanning a queue when its index is absent.
	if _, err := f.db.Exec("ALTER TABLE " + r.schema + ".ingestion_work_jobs DROP INDEX operator_history"); err != nil {
		t.Fatal(err)
	}
	if p, err := r.List(context.Background(), WorkInspectionRequest{Queue: "states", State: "completed", Limit: 10}); err == nil || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
}
