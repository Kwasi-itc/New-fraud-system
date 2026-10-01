package handlers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestErrorResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", casepkg.Invalid("name is required"), 400, "validation_error"},
		{"missing", casepkg.ErrNotFound, 404, "not_found"},
		{"missing row", pgx.ErrNoRows, 404, "not_found"},
		{"forbidden", casepkg.ErrForbidden, 403, "forbidden"},
		{"conflict", casepkg.ErrConflict, 409, "conflict"},
		{"duplicate", &pgconn.PgError{Code: "23505", Message: "secret SQL"}, 409, "conflict"},
		{"constraint", &pgconn.PgError{Code: "23503", Detail: "secret SQL"}, 400, "validation_error"},
		{"database failure", &pgconn.PgError{Code: "08006", Message: "secret SQL"}, 500, "internal_error"},
		{"unexpected", errors.New("secret SQL"), 500, "internal_error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			presentError(c, tt.err)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tt.status || body["code"] != tt.code {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret SQL") {
				t.Fatal("database details leaked")
			}
		})
	}
}

func TestNullablePatchFields(t *testing.T) {
	for _, tt := range []struct {
		body    string
		present bool
		value   *string
	}{
		{`{}`, false, nil}, {`{"field":null}`, true, nil}, {`{"field":"investigate"}`, true, strptr("investigate")},
	} {
		var body struct {
			Field nullable[string] `json:"field"`
		}
		if err := json.Unmarshal([]byte(tt.body), &body); err != nil {
			t.Fatal(err)
		}
		if body.Field.Present != tt.present || (body.Field.Value == nil) != (tt.value == nil) {
			t.Fatalf("unexpected patch state: %+v", body)
		}
		if tt.value != nil && *body.Field.Value != *tt.value {
			t.Fatal("wrong value")
		}
	}
	var body struct {
		ID nullable[uuid.UUID] `json:"id"`
	}
	if err := json.Unmarshal([]byte(`{"id":"bad-uuid"}`), &body); err == nil {
		t.Fatal("invalid UUID accepted")
	}
}

func TestInvalidCaseQueriesDoNotReachService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := CaseHandler{}
	r.GET("/tenants/:tenantId/cases", h.List)
	for _, path := range []string{
		"/tenants/00000000-0000-0000-0000-000000000000/cases",
		"/tenants/" + uuid.NewString() + "/cases?inbox_id=invalid",
		"/tenants/" + uuid.NewString() + "/cases?status=made-up",
		"/tenants/" + uuid.NewString() + "/cases?limit=invalid",
		"/tenants/" + uuid.NewString() + "/cases?limit=501",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestMalformedBodyIsValidationError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"id":`))
	var body map[string]any
	if err := bindJSON(c, &body); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func strptr(value string) *string { return &value }
