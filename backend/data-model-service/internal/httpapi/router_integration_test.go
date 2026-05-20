package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
)

func TestRouterIntegrationMainV1Flow(t *testing.T) {
	databaseURL := routerIntegrationDatabaseURL(t)
	ctx := context.Background()
	pool := routerIntegrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetRouterIntegrationDatabase(t, ctx, pool, databaseURL)

	router := NewRouter(slog.Default(), pool, RouterConfig{AuthMode: "disabled"})

	createTenantRec := doJSONRequest(t, router, http.MethodPost, "/v1/tenants", map[string]any{
		"name":         "Router Tenant",
		"external_key": "router-tenant",
	})
	if createTenantRec.Code != http.StatusCreated {
		t.Fatalf("expected create tenant 201, got %d: %s", createTenantRec.Code, createTenantRec.Body.String())
	}
	var createTenantBody struct {
		Tenant struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"tenant"`
	}
	mustUnmarshal(t, createTenantRec.Body.Bytes(), &createTenantBody)
	tenantID := createTenantBody.Tenant.ID
	if createTenantBody.Tenant.Status != "pending" {
		t.Fatalf("expected pending tenant, got %s", createTenantBody.Tenant.Status)
	}

	listTenantsRec := doRequest(t, router, http.MethodGet, "/v1/tenants", nil, "")
	if listTenantsRec.Code != http.StatusOK {
		t.Fatalf("expected list tenants 200, got %d: %s", listTenantsRec.Code, listTenantsRec.Body.String())
	}

	getTenantRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID, nil, "")
	if getTenantRec.Code != http.StatusOK {
		t.Fatalf("expected get tenant 200, got %d: %s", getTenantRec.Code, getTenantRec.Body.String())
	}

	provisionRec := doRequest(t, router, http.MethodPost, "/v1/tenants/"+tenantID+"/provision", nil, "")
	if provisionRec.Code != http.StatusOK {
		t.Fatalf("expected provision 200, got %d: %s", provisionRec.Code, provisionRec.Body.String())
	}
	var provisionBody struct {
		Tenant struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"tenant"`
	}
	mustUnmarshal(t, provisionRec.Body.Bytes(), &provisionBody)
	if provisionBody.Tenant.Status != "active" {
		t.Fatalf("expected active tenant, got %s", provisionBody.Tenant.Status)
	}

	createAccountsRec := doJSONRequest(t, router, http.MethodPost, "/v1/tenants/"+tenantID+"/tables", map[string]any{
		"name":        "accounts",
		"description": "Customer accounts",
	})
	if createAccountsRec.Code != http.StatusCreated {
		t.Fatalf("expected create accounts 201, got %d: %s", createAccountsRec.Code, createAccountsRec.Body.String())
	}
	var createAccountsBody struct {
		Table struct {
			ID string `json:"id"`
		} `json:"table"`
	}
	mustUnmarshal(t, createAccountsRec.Body.Bytes(), &createAccountsBody)

	createTransactionsRec := doJSONRequest(t, router, http.MethodPost, "/v1/tenants/"+tenantID+"/tables", map[string]any{
		"name":        "transactions",
		"description": "Transaction records",
	})
	if createTransactionsRec.Code != http.StatusCreated {
		t.Fatalf("expected create transactions 201, got %d: %s", createTransactionsRec.Code, createTransactionsRec.Body.String())
	}
	var createTransactionsBody struct {
		Table struct {
			ID string `json:"id"`
		} `json:"table"`
	}
	mustUnmarshal(t, createTransactionsRec.Body.Bytes(), &createTransactionsBody)

	listTablesRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/tables", nil, "")
	if listTablesRec.Code != http.StatusOK {
		t.Fatalf("expected list tables 200, got %d: %s", listTablesRec.Code, listTablesRec.Body.String())
	}
	var listTablesBody struct {
		Tables []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tables"`
	}
	mustUnmarshal(t, listTablesRec.Body.Bytes(), &listTablesBody)
	if len(listTablesBody.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(listTablesBody.Tables))
	}

	createFieldRec := doJSONRequest(t, router, http.MethodPost, "/v1/tables/"+createTransactionsBody.Table.ID+"/fields", map[string]any{
		"name":      "account_id",
		"data_type": "string",
		"nullable":  false,
	})
	if createFieldRec.Code != http.StatusCreated {
		t.Fatalf("expected create field 201, got %d: %s", createFieldRec.Code, createFieldRec.Body.String())
	}
	var createFieldBody struct {
		Field struct {
			ID string `json:"id"`
		} `json:"field"`
	}
	mustUnmarshal(t, createFieldRec.Body.Bytes(), &createFieldBody)

	listFieldsRec := doRequest(t, router, http.MethodGet, "/v1/tables/"+createTransactionsBody.Table.ID+"/fields", nil, "")
	if listFieldsRec.Code != http.StatusOK {
		t.Fatalf("expected list fields 200, got %d: %s", listFieldsRec.Code, listFieldsRec.Body.String())
	}
	var listFieldsBody struct {
		Fields []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"fields"`
	}
	mustUnmarshal(t, listFieldsRec.Body.Bytes(), &listFieldsBody)
	if len(listFieldsBody.Fields) < 3 {
		t.Fatalf("expected at least 3 fields on transactions table, got %d", len(listFieldsBody.Fields))
	}

	dataModelRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/data-model", nil, "")
	if dataModelRec.Code != http.StatusOK {
		t.Fatalf("expected data model 200, got %d: %s", dataModelRec.Code, dataModelRec.Body.String())
	}
	var dataModelBody struct {
		DataModel struct {
			RevisionID        string `json:"revision_id"`
			IngestionContract struct {
				TenantStatus        string   `json:"tenant_status"`
				Writable            bool     `json:"writable"`
				ManagedSystemFields []string `json:"managed_system_fields"`
				RecordLookupField   string   `json:"record_lookup_field"`
				PartialUpdates      bool     `json:"partial_updates"`
			} `json:"ingestion_contract"`
			Tables map[string]struct {
				ID       string `json:"id"`
				Archived bool   `json:"archived"`
				Fields   map[string]struct {
					ID       string `json:"id"`
					Archived bool   `json:"archived"`
				} `json:"fields"`
				LinksToSingle map[string]any `json:"links_to_single"`
			} `json:"tables"`
		} `json:"data_model"`
	}
	mustUnmarshal(t, dataModelRec.Body.Bytes(), &dataModelBody)
	if dataModelBody.DataModel.RevisionID == "" {
		t.Fatal("expected revision_id in assembled data model response")
	}
	if dataModelBody.DataModel.IngestionContract.TenantStatus != "active" {
		t.Fatalf("expected active tenant status in ingestion contract, got %s", dataModelBody.DataModel.IngestionContract.TenantStatus)
	}
	if !dataModelBody.DataModel.IngestionContract.Writable {
		t.Fatal("expected ingestion contract to mark active tenant as writable")
	}
	if dataModelBody.DataModel.IngestionContract.RecordLookupField != "object_id" {
		t.Fatalf("expected object_id record lookup field, got %s", dataModelBody.DataModel.IngestionContract.RecordLookupField)
	}
	if !dataModelBody.DataModel.IngestionContract.PartialUpdates {
		t.Fatal("expected ingestion contract to allow partial updates")
	}
	expectedManagedFields := []string{"object_id", "updated_at", "valid_from", "valid_until"}
	if len(dataModelBody.DataModel.IngestionContract.ManagedSystemFields) != len(expectedManagedFields) {
		t.Fatalf("unexpected managed system fields: %v", dataModelBody.DataModel.IngestionContract.ManagedSystemFields)
	}
	for i, fieldName := range expectedManagedFields {
		if dataModelBody.DataModel.IngestionContract.ManagedSystemFields[i] != fieldName {
			t.Fatalf("unexpected managed system fields ordering: %v", dataModelBody.DataModel.IngestionContract.ManagedSystemFields)
		}
	}
	accountObjectID := dataModelBody.DataModel.Tables["accounts"].Fields["object_id"].ID

	createLinkRec := doJSONRequest(t, router, http.MethodPost, "/v1/tenants/"+tenantID+"/links", map[string]any{
		"name":            "account",
		"parent_table_id": createAccountsBody.Table.ID,
		"parent_field_id": accountObjectID,
		"child_table_id":  createTransactionsBody.Table.ID,
		"child_field_id":  createFieldBody.Field.ID,
	})
	if createLinkRec.Code != http.StatusCreated {
		t.Fatalf("expected create link 201, got %d: %s", createLinkRec.Code, createLinkRec.Body.String())
	}
	var createLinkBody struct {
		Link struct {
			ID string `json:"id"`
		} `json:"link"`
	}
	mustUnmarshal(t, createLinkRec.Body.Bytes(), &createLinkBody)

	listLinksRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/links", nil, "")
	if listLinksRec.Code != http.StatusOK {
		t.Fatalf("expected list links 200, got %d: %s", listLinksRec.Code, listLinksRec.Body.String())
	}
	var listLinksBody struct {
		Links []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"links"`
	}
	mustUnmarshal(t, listLinksRec.Body.Bytes(), &listLinksBody)
	if len(listLinksBody.Links) != 1 || listLinksBody.Links[0].ID != createLinkBody.Link.ID {
		t.Fatalf("unexpected links payload: %+v", listLinksBody.Links)
	}

	createPivotRec := doJSONRequest(t, router, http.MethodPost, "/v1/tenants/"+tenantID+"/pivots", map[string]any{
		"base_table_id": createTransactionsBody.Table.ID,
		"path_link_ids": []string{createLinkBody.Link.ID},
	})
	if createPivotRec.Code != http.StatusCreated {
		t.Fatalf("expected create pivot 201, got %d: %s", createPivotRec.Code, createPivotRec.Body.String())
	}

	listPivotsRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/pivots", nil, "")
	if listPivotsRec.Code != http.StatusOK {
		t.Fatalf("expected list pivots 200, got %d: %s", listPivotsRec.Code, listPivotsRec.Body.String())
	}

	upsertOptionsRec := doJSONRequest(t, router, http.MethodPut, "/v1/tables/"+createTransactionsBody.Table.ID+"/options", map[string]any{
		"displayed_fields": []string{createFieldBody.Field.ID},
		"field_order":      []string{createFieldBody.Field.ID},
	})
	if upsertOptionsRec.Code != http.StatusOK {
		t.Fatalf("expected upsert options 200, got %d: %s", upsertOptionsRec.Code, upsertOptionsRec.Body.String())
	}

	getOptionsRec := doRequest(t, router, http.MethodGet, "/v1/tables/"+createTransactionsBody.Table.ID+"/options", nil, "")
	if getOptionsRec.Code != http.StatusOK {
		t.Fatalf("expected get options 200, got %d: %s", getOptionsRec.Code, getOptionsRec.Body.String())
	}
	var getOptionsBody struct {
		FieldOrder        []string `json:"field_order"`
		FieldOrderDetails []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"field_order_details"`
	}
	mustUnmarshal(t, getOptionsRec.Body.Bytes(), &getOptionsBody)
	if len(getOptionsBody.FieldOrderDetails) != len(getOptionsBody.FieldOrder) {
		t.Fatalf("expected field order details to match field order length, got %+v", getOptionsBody)
	}
	if len(getOptionsBody.FieldOrderDetails) == 0 || getOptionsBody.FieldOrderDetails[0].Name == "" {
		t.Fatalf("expected resolved field order details, got %+v", getOptionsBody.FieldOrderDetails)
	}

	dataModelAfterLinkRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/data-model", nil, "")
	if dataModelAfterLinkRec.Code != http.StatusOK {
		t.Fatalf("expected data model after link 200, got %d: %s", dataModelAfterLinkRec.Code, dataModelAfterLinkRec.Body.String())
	}
	var dataModelAfterLinkBody struct {
		DataModel struct {
			RevisionID string `json:"revision_id"`
			Tables     map[string]struct {
				Fields map[string]struct {
					ID       string `json:"id"`
					Archived bool   `json:"archived"`
				} `json:"fields"`
				LinksToSingle map[string]struct {
					ID              string `json:"id"`
					ParentTableName string `json:"parent_table_name"`
				} `json:"links_to_single"`
				Options *struct {
					FieldOrder []string `json:"field_order"`
				} `json:"options"`
			} `json:"tables"`
			Pivots []struct {
				PathLinks []string `json:"path_links"`
			} `json:"pivots"`
		} `json:"data_model"`
	}
	mustUnmarshal(t, dataModelAfterLinkRec.Body.Bytes(), &dataModelAfterLinkBody)
	if dataModelAfterLinkBody.DataModel.RevisionID == "" {
		t.Fatal("expected revision_id after link creation")
	}
	if dataModelAfterLinkBody.DataModel.RevisionID == dataModelBody.DataModel.RevisionID {
		t.Fatal("expected revision_id to change after schema-affecting mutations")
	}
	txTable := dataModelAfterLinkBody.DataModel.Tables["transactions"]
	if _, ok := txTable.Fields["account_id"]; !ok {
		t.Fatal("expected account_id field in assembled data model")
	}
	if txTable.Fields["account_id"].Archived {
		t.Fatal("expected active field to report archived=false")
	}
	if link, ok := txTable.LinksToSingle["account"]; !ok || link.ParentTableName != "accounts" {
		t.Fatalf("unexpected links_to_single payload: %+v", txTable.LinksToSingle)
	}
	if txTable.Options == nil || len(txTable.Options.FieldOrder) != 1 || txTable.Options.FieldOrder[0] != createFieldBody.Field.ID {
		t.Fatalf("unexpected options payload: %+v", txTable.Options)
	}
	if len(dataModelAfterLinkBody.DataModel.Pivots) != 1 || len(dataModelAfterLinkBody.DataModel.Pivots[0].PathLinks) != 1 || dataModelAfterLinkBody.DataModel.Pivots[0].PathLinks[0] != "account" {
		t.Fatalf("unexpected pivots payload: %+v", dataModelAfterLinkBody.DataModel.Pivots)
	}

	schemaChangesRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/schema-change-log", nil, "")
	if schemaChangesRec.Code != http.StatusOK {
		t.Fatalf("expected schema change log 200, got %d: %s", schemaChangesRec.Code, schemaChangesRec.Body.String())
	}
	var schemaChangesBody struct {
		SchemaChanges []any `json:"schema_changes"`
	}
	mustUnmarshal(t, schemaChangesRec.Body.Bytes(), &schemaChangesBody)
	if len(schemaChangesBody.SchemaChanges) == 0 {
		t.Fatal("expected schema changes in response")
	}

	schemaMigrationsRec := doRequest(t, router, http.MethodGet, "/v1/tenants/"+tenantID+"/schema-migrations", nil, "")
	if schemaMigrationsRec.Code != http.StatusOK {
		t.Fatalf("expected schema migrations 200, got %d: %s", schemaMigrationsRec.Code, schemaMigrationsRec.Body.String())
	}
	var schemaMigrationsBody struct {
		TenantSchemaMigrations []any `json:"tenant_schema_migrations"`
	}
	mustUnmarshal(t, schemaMigrationsRec.Body.Bytes(), &schemaMigrationsBody)
	if len(schemaMigrationsBody.TenantSchemaMigrations) == 0 {
		t.Fatal("expected tenant schema migrations in response")
	}

	reconcileRec := doRequest(t, router, http.MethodGet, "/v1/admin/reconcile", nil, "")
	if reconcileRec.Code != http.StatusOK {
		t.Fatalf("expected reconcile 200, got %d: %s", reconcileRec.Code, reconcileRec.Body.String())
	}
}

func doJSONRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return doRequest(t, router, method, path, payload, "application/json")
}

func doRequest(t *testing.T, router http.Handler, method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func mustUnmarshal(t *testing.T, body []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, string(body))
	}
}

func routerIntegrationDatabaseURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DATA_MODEL_TEST_DATABASE_URL"); url != "" {
		return url
	}
	t.Skip("set DATA_MODEL_TEST_DATABASE_URL to run PostgreSQL integration tests")
	return ""
}

func routerIntegrationPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := storepostgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect integration pool: %v", err)
	}
	return pool
}

func resetRouterIntegrationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseURL string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT nspname
		FROM pg_namespace
		WHERE nspname = 'core' OR nspname LIKE 'tenant_%'
	`)
	if err != nil {
		t.Fatalf("list schemas: %v", err)
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			t.Fatalf("scan schema: %v", err)
		}
		schemas = append(schemas, schema)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schemas: %v", err)
	}
	for _, schema := range schemas {
		if _, err := pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgx.Identifier{schema}.Sanitize())); err != nil {
			t.Fatalf("drop schema %s: %v", schema, err)
		}
	}
	runRouterMetadataMigrations(t, databaseURL)
}

func runRouterMetadataMigrations(t *testing.T, databaseURL string) {
	t.Helper()
	_, fileName, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}
	rootDir := filepath.Clean(filepath.Join(filepath.Dir(fileName), "..", ".."))
	migrationsPath := "file://" + filepath.ToSlash(filepath.Join(rootDir, "internal", "migrations", "metadata"))
	m, err := migrate.New(migrationsPath, databaseURL)
	if err != nil {
		t.Fatalf("create migrate client: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}
}
