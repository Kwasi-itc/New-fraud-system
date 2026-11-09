package mysqlruntime

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestLocalRouteInventory(t *testing.T) {
	connector, err := mysql.NewConnector(mysql.NewConfig())
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	router, err := newLocalRouter(db)
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, route := range router.Routes() {
		actual = append(actual, route.Method+" "+route.Path)
	}
	expected := []string{
		"POST /v1/tenants/:tenantId/links",
		"DELETE /v1/links/:linkId",
		"POST /v1/tenants/:tenantId/pivots",
		"DELETE /v1/pivots/:pivotId",
		"PUT /v1/tables/:tableId/options",
		"POST /v1/tables/:tableId/navigation-options",
		"DELETE /v1/navigation-options/:navigationOptionId",
		"POST /v1/tenants/:tenantId/index-jobs",
		"GET /v1/tenants/:tenantId/index-jobs/scoped",
		"POST /v1/index-jobs/:jobId/retry",
		"GET /v1/admin/reconcile",

		"DELETE /v1/tables/:tableId", "POST /v1/fields/:fieldId/enum-values",
		"GET /v1/tenants/:tenantId/data-model/export", "POST /v1/tenants/:tenantId/data-model/import",
		"DELETE /v1/fields/:fieldId", "PATCH /v1/enum-values/:enumValueId", "DELETE /v1/enum-values/:enumValueId",
		"GET /v1/tables/:tableId/display-settings", "PUT /v1/tables/:tableId/display-settings", "POST /v1/tenants/:tenantId/record-displays",
		"GET /docs", "GET /openapi.json", "GET /openapi.yaml", "GET /healthz", "GET /readyz",
		"GET /v1/tenants", "POST /v1/tenants", "GET /v1/tenants/:tenantId", "POST /v1/tenants/:tenantId/provision",
		"GET /v1/tenants/:tenantId/data-model", "GET /v1/tenants/:tenantId/tables", "POST /v1/tenants/:tenantId/tables", "PATCH /v1/tables/:tableId",
		"GET /v1/tables/:tableId/fields", "POST /v1/tables/:tableId/fields", "PATCH /v1/fields/:fieldId", "GET /v1/fields/:fieldId/enum-values",
		"GET /v1/tenants/:tenantId/links", "GET /v1/tenants/:tenantId/pivots", "GET /v1/tables/:tableId/options", "GET /v1/tables/:tableId/navigation-options",
		"GET /v1/tenants/:tenantId/schema-change-log", "GET /v1/tenants/:tenantId/schema-migrations", "GET /v1/tenants/:tenantId/index-jobs", "GET /v1/index-jobs/:jobId",
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("routes changed\ngot: %v\nwant: %v", actual, expected)
	}
}

type authorWorkflowStub struct {
	displayGet   func(context.Context, uuid.UUID) (display.Settings, error)
	displaySave  func(context.Context, uuid.UUID, display.SaveRequest, uuid.UUID) (display.Settings, error)
	displayRead  func(context.Context, uuid.UUID, []display.Object) (display.Batch, error)
	fieldDelete  func(context.Context, uuid.UUID, bool, uuid.UUID) (datamodel.DeleteReport, error)
	enumMutation func(context.Context, service.UpdateFieldEnumValueInput, bool, uuid.UUID) (datamodel.FieldEnumValue, error)
	create       func(context.Context, service.CreateTableInput, uuid.UUID) (datamodel.Table, error)
	field        func(context.Context, service.CreateFieldInput, uuid.UUID) (datamodel.Field, error)
	tableUpdate  func(context.Context, service.UpdateTableInput, uuid.UUID) (datamodel.Table, error)
	fieldUpdate  func(context.Context, service.UpdateFieldInput, uuid.UUID) (datamodel.Field, error)
}

func (s authorWorkflowStub) Create(c context.Context, i service.CreateTableInput, k uuid.UUID) (datamodel.Table, error) {
	return s.create(c, i, k)
}
func (s authorWorkflowStub) CreateField(c context.Context, i service.CreateFieldInput, k uuid.UUID) (datamodel.Field, error) {
	return s.field(c, i, k)
}
func (s authorWorkflowStub) UpdateTable(c context.Context, i service.UpdateTableInput, k uuid.UUID) (datamodel.Table, error) {
	return s.tableUpdate(c, i, k)
}
func (s authorWorkflowStub) UpdateField(c context.Context, i service.UpdateFieldInput, k uuid.UUID) (datamodel.Field, error) {
	return s.fieldUpdate(c, i, k)
}

func TestLocalAuthorHandlersInvokeScopedWorkflow(t *testing.T) {
	id, key := uuid.New(), uuid.New()
	calls := 0
	check := func(ctx context.Context, scope, operation uuid.UUID) {
		t.Helper()
		calls++
		if localAuthority(ctx, nil) != nil || scope != id || operation != key {
			t.Fatal("lost trusted scope or operation")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("request timeout lost")
		}
	}
	worker := authorWorkflowStub{
		create: func(ctx context.Context, in service.CreateTableInput, op uuid.UUID) (datamodel.Table, error) {
			check(ctx, in.TenantID, op)
			if in.Name != "records" {
				t.Fatal(in)
			}
			return datamodel.Table{ID: id, Name: in.Name}, nil
		},
		field: func(ctx context.Context, in service.CreateFieldInput, op uuid.UUID) (datamodel.Field, error) {
			check(ctx, in.TableID, op)
			if in.Name != "score" || in.DataType != datamodel.DataTypeInt || !in.Nullable {
				t.Fatal(in)
			}
			return datamodel.Field{ID: id, Name: in.Name}, nil
		},
		tableUpdate: func(ctx context.Context, in service.UpdateTableInput, op uuid.UUID) (datamodel.Table, error) {
			check(ctx, in.TableID, op)
			if in.Alias == nil || *in.Alias != "Edited" {
				t.Fatal(in)
			}
			return datamodel.Table{ID: id, Alias: *in.Alias}, nil
		},
		fieldUpdate: func(ctx context.Context, in service.UpdateFieldInput, op uuid.UUID) (datamodel.Field, error) {
			check(ctx, in.FieldID, op)
			if in.Alias == nil || *in.Alias != "Edited" {
				t.Fatal(in)
			}
			return datamodel.Field{ID: id, Alias: *in.Alias}, nil
		},
	}
	secret := strings.Repeat("s", 64)
	router, err := newLocalAuthorHTTP(secret, worker)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/v1/tenants/" + id.String() + "/tables", `{"name":"records"}`, 201},
		{"POST", "/v1/tables/" + id.String() + "/fields", `{"name":"score","data_type":"int","nullable":true}`, 201},
		{"PATCH", "/v1/tables/" + id.String(), `{"alias":"Edited"}`, 200},
		{"PATCH", "/v1/fields/" + id.String(), `{"alias":"Edited"}`, 200},
	} {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+secret)
		request.Header.Set("Idempotency-Key", key.String())
		request.Header.Set("X-Tenant-ID", uuid.NewString())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status || response.Header().Get("Idempotency-Key") != key.String() || !strings.Contains(response.Body.String(), id.String()) {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if calls != 4 {
		t.Fatal("lost route dispatch", calls)
	}
}

func (s authorWorkflowStub) DeleteField(c context.Context, id uuid.UUID, dry bool, op uuid.UUID) (datamodel.DeleteReport, error) {
	return s.fieldDelete(c, id, dry, op)
}
func (s authorWorkflowStub) MutateEnum(c context.Context, in service.UpdateFieldEnumValueInput, remove bool, op uuid.UUID) (datamodel.FieldEnumValue, error) {
	return s.enumMutation(c, in, remove, op)
}

func (s authorWorkflowStub) DisplaySettings(c context.Context, id uuid.UUID) (display.Settings, error) {
	return s.displayGet(c, id)
}
func (s authorWorkflowStub) SaveDisplaySettings(c context.Context, id uuid.UUID, in display.SaveRequest, op uuid.UUID) (display.Settings, error) {
	return s.displaySave(c, id, in, op)
}
func (s authorWorkflowStub) RecordDisplays(c context.Context, id uuid.UUID, in []display.Object) (display.Batch, error) {
	return s.displayRead(c, id, in)
}

func (s authorWorkflowStub) ExportModel(c context.Context, id uuid.UUID) (service.PortableDataModelDocument, error) {
	return service.PortableDataModelDocument{Version: "v1"}, nil
}
func (s authorWorkflowStub) ImportModel(c context.Context, id uuid.UUID, d service.PortableDataModelDocument, key uuid.UUID) (service.PortableImportResult, error) {
	return service.PortableImportResult{TablesCreated: len(d.Tables), RevisionID: "test"}, nil
}

func (s authorWorkflowStub) DeleteTable(c context.Context, id uuid.UUID, dry bool, op uuid.UUID) (datamodel.DeleteReport, error) {
	return datamodel.NewDeleteReport(), nil
}
func (s authorWorkflowStub) CreateEnum(c context.Context, in service.CreateFieldEnumValueInput, op uuid.UUID) (datamodel.FieldEnumValue, error) {
	return datamodel.FieldEnumValue{ID: op, FieldID: in.FieldID, Value: in.Value, Label: in.Label}, nil
}
