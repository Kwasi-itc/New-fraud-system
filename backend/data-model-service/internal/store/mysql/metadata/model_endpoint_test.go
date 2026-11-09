package metadata_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/mysqlruntime"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestStagedPublishedModelEndpoint(t *testing.T) {
	db, database := metadataDB(t)
	ctx := context.Background()
	id, tableID, version := uuid.New(), uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, id, 100, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(id, at, "endpoint", nil)); err != nil {
			return err
		}
		if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: id, Name: "events", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		registry, err := store.NewPhysicalRegistry(tx, database, id, 100)
		if err != nil {
			return err
		}
		return registry.RegisterTable(ctx, tableID)
	}); err != nil {
		t.Fatal(err)
	}
	// Privileged fixture sets an eligible registry token. This proves endpoint
	// composition, not actual physical index publication (D13.14/D13.16).
	setGate := func(gate string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET published_version=?,write_state=? WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(version), gate, mysqlstore.UUID(id), mysqlstore.UUID(tableID)); err != nil {
			t.Fatal(err)
		}
	}
	setGate("open")
	type trustedKey struct{}
	calls := 0
	authority := func(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID) error {
		calls++
		if tenantID != id || ctx.Value(trustedKey{}) != true {
			return access.Denied()
		}
		var selected string
		if err := tx.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selected); err != nil {
			return err
		}
		if selected != database {
			return errors.New("wrong shared database")
		}
		return nil
	}
	if _, err := mysqlruntime.NewModelEndpoint(db, database, id, 100, 1<<20, nil); err == nil {
		t.Fatal("missing authority accepted")
	}
	handler, err := mysqlruntime.NewModelEndpoint(db, database, id, 100, 1<<20, authority)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/data-model", handler)
	request := func(path, etag string, trusted bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+path+"/data-model", nil)
		req = req.WithContext(context.WithValue(req.Context(), trustedKey{}, trusted))
		req.Header.Set("If-None-Match", etag)
		req.Header.Set("X-Tenant-ID", id.String()) // Never grants authority.
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	first := request(id.String(), "", true)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var body struct {
		Model dto.AssembledDataModelResponse `json:"data_model"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Model.Tables["events"].PublishedVersion
	if got == nil || *got != version {
		t.Fatalf("missing eligible registry token: %s", first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing cache validator")
	}
	if result := request(id.String(), etag, true); result.Code != 304 {
		t.Fatal(result.Code)
	}
	if result := request(id.String(), etag, false); result.Code != 403 || result.Header().Get("ETag") != "" {
		t.Fatal("conditional request bypassed authority", result.Code)
	}
	before := calls
	if result := request(uuid.NewString(), "", true); result.Code != 403 || calls != before {
		t.Fatal("foreign path reached reader", result.Code)
	}
	if result := request("bad", "", true); result.Code != 400 {
		t.Fatal(result.Code)
	}
	setGate("closed")
	closed := request(id.String(), etag, true)
	if closed.Code != 200 || closed.Header().Get("ETag") == etag || strings.Contains(closed.Body.String(), "published_version") {
		t.Fatal("stale publication advertised", closed.Code, closed.Body.String())
	}
	failed, err := mysqlruntime.NewModelEndpoint(db, database, id, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return errors.New("private SQL schema detail") })
	if err != nil {
		t.Fatal(err)
	}
	router = gin.New()
	router.GET("/v1/tenants/:tenantId/data-model", failed)
	result := request(id.String(), "*", true)
	if result.Code != 503 || strings.Contains(result.Body.String(), "private") {
		t.Fatal("storage error leaked or succeeded", result.Code, result.Body.String())
	}
}
