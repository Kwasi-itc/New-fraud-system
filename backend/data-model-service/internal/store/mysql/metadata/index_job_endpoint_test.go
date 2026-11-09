package metadata_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/mysqlruntime"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestStagedIndexJobEndpoints(t *testing.T) {
	db, database := metadataDB(t)
	ctx := context.Background()
	id, table := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, id, 100, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(id, at, "index endpoint", nil)); err != nil {
			return err
		}
		if err := r.Tenants.UpdateStatus(ctx, id, tenant.StatusActive); err != nil {
			return err
		}
		if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: id, Name: "events", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		return r.Fields.Create(ctx, datamodel.Field{ID: uuid.New(), TenantID: id, TableID: table, Name: "amount", DataType: datamodel.DataTypeInt, CreatedAt: at, UpdatedAt: at})
	})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.NewIndexDeliveryStore(db, database, "endpoint", 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	type trustedKey struct{}
	authorize := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
		if tenant != id || ctx.Value(trustedKey{}) != true {
			return access.Denied()
		}
		var probe int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&probe)
	}
	endpoints, err := mysqlruntime.NewIndexJobEndpoints(db, database, id, 100, 1<<20, authorize, authorize, queue, &registrationIDs{}, registrationClock{at})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/index-jobs/scoped", endpoints.Scoped)
	router.POST("/v1/tenants/:tenantId/index-jobs", endpoints.Create)
	router.POST("/v1/index-jobs/:jobId/retry", endpoints.Retry)
	request := func(method, path, body string, trusted bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), trustedKey{}, trusted))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	base := "/v1/tenants/" + id.String() + "/index-jobs"
	payload := fmt.Sprintf(`{"table_id":%q,"index_type":"search","columns":["amount"]}`, table.String())
	count := func(tableName string) int {
		t.Helper()
		// Only fixed table names from this test are passed here.
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tableName+" WHERE tenant_id=?", mysqlstore.UUID(id)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	failEnqueue := func() {
		t.Helper()
		if _, err := db.ExecContext(ctx, "CREATE TRIGGER fixture_fail_enqueue BEFORE INSERT ON data_model_index_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private enqueue fault'"); err != nil {
			t.Fatal(err)
		}
	}
	repairEnqueue := func() {
		t.Helper()
		if _, err := db.ExecContext(ctx, "DROP TRIGGER fixture_fail_enqueue"); err != nil {
			t.Fatal(err)
		}
	}
	if out := request("POST", base, payload, false); out.Code != 403 {
		t.Fatal("untrusted create accepted", out.Code)
	}
	failEnqueue()
	if out := request("POST", base, payload, true); out.Code != 503 || strings.Contains(out.Body.String(), "private enqueue") {
		t.Fatal("failed enqueue reported success")
	}
	for _, tableName := range []string{"data_model_index_jobs", "data_model_schema_change_log", "data_model_index_deliveries", "data_model_index_delivery_reservations"} {
		if count(tableName) != 0 {
			t.Fatal("enqueue failure did not roll back", tableName)
		}
	}
	repairEnqueue()
	created := request("POST", base, payload, true)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var body struct {
		Job struct {
			ID uuid.UUID `json:"id"`
		} `json:"index_job"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil || body.Job.ID == uuid.Nil {
		t.Fatal(err, created.Body.String())
	}
	job := body.Job.ID
	for _, tableName := range []string{"data_model_index_jobs", "data_model_schema_change_log", "data_model_index_deliveries", "data_model_index_delivery_reservations"} {
		if count(tableName) != 1 {
			t.Fatal("missing committed job component", tableName)
		}
	}
	listed := request("GET", base+"/scoped?table_id="+table.String()+"&limit=10", "", true)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), job.String()) {
		t.Fatal(listed.Code, listed.Body.String())
	}
	if out := request("GET", base+"/scoped?table_id="+table.String()+"&limit=10", "", false); out.Code != 403 {
		t.Fatal("untrusted read accepted", out.Code)
	}
	if out := request("POST", "/v1/tenants/"+uuid.NewString()+"/index-jobs", payload, true); out.Code != 403 {
		t.Fatal("foreign path accepted", out.Code)
	}
	lease, err := queue.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	// Real queue completion, with the owner failure in the same transaction.
	if err := queue.Finish(ctx, *lease, false, 0, "", func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, id, 100, 1<<20)
		if err != nil {
			return err
		}
		return r.Jobs.MarkFailed(ctx, job, "fixture failure", at)
	}); err != nil {
		t.Fatal(err)
	}
	retry := "/v1/index-jobs/" + job.String() + "/retry"
	if out := request("POST", retry, "", false); out.Code != 403 {
		t.Fatal("untrusted retry accepted", out.Code)
	}
	if out := request("POST", "/v1/index-jobs/"+uuid.NewString()+"/retry", "", true); out.Code != 404 {
		t.Fatal("missing/foreign retry leaked", out.Code)
	}
	failEnqueue()
	if out := request("POST", retry, "", true); out.Code == 200 {
		t.Fatal("failed retry enqueue reported success")
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(job)).Scan(&status); err != nil || status != "failed" {
		t.Fatal("failed retry changed job", status, err)
	}
	if count("data_model_index_deliveries") != 1 || count("data_model_index_delivery_reservations") != 1 {
		t.Fatal("retry rollback lost retained delivery/reservation")
	}
	repairEnqueue()
	// The job lock prevents simultaneous retries creating two new deliveries.
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- request("POST", retry, "", true).Code }()
	}
	wg.Wait()
	close(codes)
	successes := 0
	for code := range codes {
		if code == 200 {
			successes++
		} else if code != 409 {
			t.Fatal("unexpected concurrent retry response", code)
		}
	}
	if successes != 1 || count("data_model_index_deliveries") != 2 || count("data_model_index_delivery_reservations") != 1 {
		t.Fatal("concurrent retry duplicated or lost delivery", successes)
	}
	next, err := queue.Claim(ctx)
	if err != nil || next == nil || next.ID == lease.ID || next.JobID != job {
		t.Fatal("retry did not produce fresh claimable delivery", next, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&status); err != nil || status != "completed" {
		t.Fatal("old delivery evidence changed", status, err)
	}
	// A worker error may leave a failed domain job with an available transport
	// retry. Owner retry must reuse that unleased delivery, not duplicate it.
	if err := queue.Finish(ctx, *next, true, 0, "fixture_failure", func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, id, 100, 1<<20)
		if err != nil {
			return err
		}
		return r.Jobs.MarkFailed(ctx, job, "fixture retry", at)
	}); err != nil {
		t.Fatal(err)
	}
	if out := request("POST", retry, "", true); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if count("data_model_index_deliveries") != 2 {
		t.Fatal("available delivery was duplicated")
	}
	reused, err := queue.Claim(ctx)
	if err != nil || reused == nil || reused.ID != next.ID || reused.Token == next.Token {
		t.Fatal("unleased retry was not reclaimed", reused, err)
	}
}
