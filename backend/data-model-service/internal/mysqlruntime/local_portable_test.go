package mysqlruntime

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/google/uuid"
)

func TestPortablePreflightRejectsWholeInvalidGraph(t *testing.T) {
	base := service.PortableDataModelDocument{Version: "v1", Tables: []service.PortableTable{{Name: "accounts", Fields: []service.PortableField{{Name: "name", Alias: "Customer", DataType: "string", Nullable: true}}}}}
	raw, _ := json.Marshal(base)
	for _, test := range []struct {
		name   string
		change func(*service.PortableDataModelDocument)
	}{
		{"unsupported type", func(d *service.PortableDataModelDocument) { d.Tables[0].Fields[0].DataType = "money" }},
		{"missing caption", func(d *service.PortableDataModelDocument) { d.Tables[0].CaptionField = "unknown" }},
		{"duplicate names", func(d *service.PortableDataModelDocument) { d.Tables = append(d.Tables, d.Tables[0]) }},
		{"bad enum", func(d *service.PortableDataModelDocument) {
			d.Tables[0].Fields[0].EnumValues = []service.CreateFieldEnumValueSeed{{Value: "a"}}
		}},
		{"invalid pivot", func(d *service.PortableDataModelDocument) {
			d.Pivots = []service.PortablePivot{{BaseTable: "accounts"}}
		}},
		{"unbacked navigation", func(d *service.PortableDataModelDocument) {
			d.Tables[0].NavigationOptions = []service.PortableNavigationOption{{SourceField: "name", TargetTable: "accounts", FilterField: "name", OrderingField: "updated_at"}}
		}},
		{"duplicate options", func(d *service.PortableDataModelDocument) {
			d.Tables[0].Options = &service.PortableTableOptions{FieldOrder: []string{"name", "name"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var d service.PortableDataModelDocument
			json.Unmarshal(raw, &d)
			test.change(&d)
			if validateLocalPortable(d) == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
	if err := validateLocalPortable(base); err != nil {
		t.Fatal(err)
	}
	adapted := handlers.AdaptPortableDocumentRequest(handlers.AdaptPortableDocument(base))
	if adapted.Tables[0].Fields[0].Alias != "Customer" {
		t.Fatal("field alias lost")
	}
}

func TestLocalPortableHTTP(t *testing.T) {
	secret := strings.Repeat("s", 64)
	router, err := newLocalAuthorHTTP(secret, authorWorkflowStub{})
	if err != nil {
		t.Fatal(err)
	}
	id, key := uuid.NewString(), uuid.NewString()
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "export", "", 200},
		{"POST", "import", `{"data_model":{"version":"v1","tables":[]}}`, 200},
		{"POST", "import", `{}`, 400},
		{"POST", "import", `{"data_model":null}`, 400},
		{"POST", "import", `{"data_model":{"version":"future"}}`, 400},
		{"POST", "import", `{"data_model":{"tables":[],"unknown":true}}`, 400},
		{"POST", "import", `{"data_model":{"tables":[]}} {}`, 400},
	} {
		r := httptest.NewRequest(test.method, "/v1/tenants/"+id+"/data-model/"+test.path, strings.NewReader(test.body))
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatal(test, w.Code, w.Body.String())
		}
		if test.method == "POST" && test.status == 200 {
			var result dto.PortableDataModelImportResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Summary.RevisionID != "test" {
				t.Fatal(w.Body.String())
			}
		}
	}
}
