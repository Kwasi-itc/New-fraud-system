package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Seed only the generated fixture, with mixed kinds and tied timestamps. These
// are terminal jobs: no active reservations or worker side effects are invented.
func seedUploadList(t *testing.T, f recordFixtureState, w *WorkOwner, n int) {
	t.Helper()
	for start := 0; start < n; start += 100 {
		end := start + 100
		if end > n {
			end = n
		}
		var resources, jobs []string
		var ra, ja []any
		for i := start; i < end; i++ {
			id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("upload-list-%d", i)))
			kind := UploadWork
			if i%2 == 1 {
				kind = DeferredWork
			}
			at := mysqlstore.DateTime{Time: time.Date(2020, 1, 1, 0, 0, i/100, 0, time.UTC)}
			resources = append(resources, "(?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
			ra = append(ra, mysqlstore.UUID(id), mysqlstore.UUID(w.owner.receipts.tenant), mysqlstore.UUID(w.owner.table), w.owner.objectType, kind, "patch", []byte(strings.Repeat("private", 200)), "file.csv", "text/csv", at, at, at, 1, 1)
			jobs = append(jobs, "(?,?,?,?,?,1,'{}','completed',?,1,3,?,?,?)")
			ja = append(ja, mysqlstore.UUID(id), mysqlstore.UUID(w.owner.receipts.tenant), mysqlstore.UUID(id), kind, "uploads", at, at, at, at)
		}
		err := mysqlstore.Mutation(context.Background(), f.db, func(tx *sql.Tx) error {
			s := w.owner.receipts.schema
			if _, err := tx.Exec("INSERT INTO "+s+".ingestion_work_resources(id,tenant_id,table_id,object_type,kind,mode,payload,filename,content_type,scheduled_at,created_at,completed_at,total_rows,successful_rows) VALUES "+strings.Join(resources, ","), ra...); err != nil {
				return err
			}
			_, err := tx.Exec("INSERT INTO "+s+".ingestion_work_jobs(id,tenant_id,resource_id,kind,queue,payload_version,payload,state,available_at,attempt,max_attempts,created_at,started_at,finished_at) VALUES "+strings.Join(jobs, ","), ja...)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUploadListPaginationAndPlan(t *testing.T) {
	f, w := workFixture(t)
	seedUploadList(t, f, w, 2000)
	ctx := context.Background()
	seen := map[uuid.UUID]bool{}
	var cursor *UploadCursor
	var previous *UploadSummary
	for {
		page, err := w.ListUploads(ctx, 37, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 37 {
			t.Fatal("page limit")
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate across pages", item.ID)
			}
			seen[item.ID] = true
			if previous != nil && (item.RequestedAt.After(previous.RequestedAt) || (item.RequestedAt.Equal(previous.RequestedAt) && strings.Compare(item.ID.String(), previous.ID.String()) >= 0)) {
				t.Fatal("unstable order")
			}
			if item.State != "completed" || item.Attempt != 1 || item.Filename != "file.csv" || item.TotalRows != 1 || item.SuccessfulRows != 1 || item.StartedAt == nil || item.CompletedAt == nil {
				t.Fatal(item)
			}
			v := item
			previous = &v
		}
		if page.Next == nil {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 1000 {
		t.Fatal("missing uploads or included deferred work", len(seen))
	}
	// Evidence uses the actual builder and representative mixed history, with
	// both first-page and continuation predicates. The production hint prevents
	// the observed optimizer choice of a kind-agnostic history scan.
	if _, err := f.db.Exec("ANALYZE TABLE " + w.owner.receipts.schema + ".ingestion_work_resources"); err != nil {
		t.Fatal(err)
	}
	for _, position := range []*UploadCursor{nil, cursor} {
		query, args := w.uploadListQuery(38, position)
		var raw string
		if err := f.db.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plan any
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			t.Fatal(err)
		}
		indexed := false
		var visit func(any)
		visit = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if v["table_name"] == "r" && v["key"] == "upload_history" && (v["access_type"] == "ref" || v["access_type"] == "range") {
					indexed = true
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
		t.Log("upload history plan:", raw)
		if !indexed {
			t.Fatal("expected scoped upload_history access")
		}
	}
}

func TestUploadListAuthorizationAndProjection(t *testing.T) {
	f, w := workFixture(t)
	seedUploadList(t, f, w, 4)
	ctx := context.Background()
	for _, limit := range []int{0, 101} {
		if _, err := w.ListUploads(ctx, limit, nil); err == nil {
			t.Fatal("invalid limit")
		}
	}
	for _, cursor := range []*UploadCursor{{TenantID: uuid.New(), TableID: w.owner.table, ID: uuid.New(), CreatedAt: time.Now()}, {TenantID: w.owner.receipts.tenant, TableID: w.owner.table, ID: uuid.New()}} {
		if _, err := w.ListUploads(ctx, 1, cursor); err == nil {
			t.Fatal("invalid cursor")
		}
	}
	original := w.owner.receipts.authorizeTx
	w.owner.receipts.authorizeTx = func(_ context.Context, _ *sql.Tx, r ReceiptRequest) error {
		if r.Mode == string(ingestion.ModeCreate) {
			return ErrOwnerAuthorization
		}
		return nil
	}
	if page, err := w.ListUploads(ctx, 10, nil); !errors.Is(err, ErrOwnerAuthorization) || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	w.owner.receipts.authorizeTx = original
	tenant := w.owner.receipts.tenant
	w.owner.receipts.tenant = uuid.New()
	if page, err := w.ListUploads(ctx, 10, nil); err != nil || len(page.Items) != 0 {
		t.Fatal("tenant isolation", page, err)
	}
	w.owner.receipts.tenant = tenant
	table := w.owner.table
	w.owner.table = uuid.New()
	if page, err := w.ListUploads(ctx, 10, nil); err != nil || len(page.Items) != 0 {
		t.Fatal("table isolation", page, err)
	}
	w.owner.table = table
	pool := restrictedReceiptPool(t, f.db, f.database)
	var login string
	if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	account := "'" + strings.Split(login, "@")[0] + "'@'%'"
	s := w.owner.receipts.schema
	for _, q := range []string{
		"GRANT SELECT(id,tenant_id,table_id,current_job_id,kind,mode,filename,content_type,total_rows,successful_rows,failed_rows,created_at,completed_at) ON " + s + ".ingestion_work_resources TO " + account,
		"GRANT SELECT(id,tenant_id,resource_id,state,attempt,started_at,finished_at,last_error) ON " + s + ".ingestion_work_jobs TO " + account,
	} {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	w.owner.receipts.db = pool
	page, err := w.ListUploads(ctx, 10, nil)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	for _, column := range []string{"payload", "request_key", "result_payload"} {
		if _, err := pool.Query("SELECT " + column + " FROM " + s + ".ingestion_work_resources"); err == nil {
			t.Fatal("sensitive projection granted", column)
		}
	}
	// Malformed retained metadata must fail, rather than returning a partial page
	// or transferring arbitrarily large strings to application memory.
	if _, err := f.db.Exec("UPDATE "+s+".ingestion_work_resources SET filename=REPEAT('x',5000) WHERE kind=?", UploadWork); err != nil {
		t.Fatal(err)
	}
	if page, err := w.ListUploads(ctx, 10, nil); err == nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}

func TestUploadListMissingJobFailsClosed(t *testing.T) {
	f, w := workFixture(t)
	seedUploadList(t, f, w, 4)
	if _, err := f.db.Exec("DELETE FROM "+w.owner.receipts.schema+".ingestion_work_jobs WHERE kind=? LIMIT 1", UploadWork); err != nil {
		t.Fatal(err)
	}
	if page, err := w.ListUploads(context.Background(), 10, nil); err == nil || len(page.Items) != 0 {
		t.Fatal("missing job hidden", page, err)
	}
}
