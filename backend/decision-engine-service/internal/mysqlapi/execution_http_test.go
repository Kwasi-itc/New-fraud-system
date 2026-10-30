package mysqlapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestExecutionHTTPStrictDecoding(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"expected_generation":null}`, `{"expected_generation":-1}`,
		`{"expected_generation":0,"expected_generation":1}`, `{"expected_generation":0} {}`,
		`{"expected_generation":0,"tenant_id":"forged"}`, `{"expected_generation":2147483648}`,
	} {
		var request CancellationRequest
		err := decodeExecutionHTTP(strings.NewReader(raw), 1024, &request)
		if err == nil {
			err = request.Validate()
		}
		if err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var object struct {
		Fields map[string]any `json:"fields"`
	}
	if err := decodeExecutionHTTP(strings.NewReader(`{"fields":{"value":1,"value":2}}`), 1024, &object); err == nil {
		t.Fatal("duplicate nested field")
	}
	if err := decodeExecutionHTTP(strings.NewReader(`{"fields":{"value":9007199254740993}}`), 1024, &object); err != nil {
		t.Fatal(err)
	}
	if object.Fields["value"] != json.Number("9007199254740993") {
		t.Fatal("lost numeric precision", object)
	}
	if err := decodeExecutionHTTP(strings.NewReader(`{"fields":`+strings.Repeat(`[`, 66)+`0`+strings.Repeat(`]`, 66)+`}`), 4096, &object); !errors.Is(err, errExecutionHTTPSize) {
		t.Fatal("depth", err)
	}
	if err := decodeExecutionHTTP(strings.NewReader(`{"fields":{}}`+strings.Repeat(" ", 1024)), 1024, &object); !errors.Is(err, errExecutionHTTPSize) {
		t.Fatal("trailing size", err)
	}
}

func TestExecutionHTTPFailClosedAndRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cause := range []error{mysql.ErrDecisionAuthorizationDenied, mysql.ErrDecisionAuthorizationUnavailable, errors.New("password=secret SQL internals")} {
		h, err := NewExecutionHTTP(func(context.Context, uuid.UUID) (*ExecutionEndpointScope, error) { return nil, cause }, 4096)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		h.Register(router.Group("/v1"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/tenants/"+uuid.NewString()+"/async-decision-executions/"+uuid.NewString(), nil))
		if rec.Code < 400 || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "SQL") {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	if _, err := NewExecutionHTTP(nil, 1024); err == nil {
		t.Fatal("missing trusted factory")
	}
}

func TestExecutionHTTPErrorClassification(t *testing.T) {
	id := uuid.NewString()
	for _, tc := range []struct {
		err    error
		status int
		id     string
	}{
		{mysql.ErrDecisionAuthorizationDenied, 403, ""}, {sql.ErrNoRows, 404, ""},
		{mysql.ErrExecutionState, 409, ""}, {service.ErrExecutionPreparation, 422, ""},
		{mysql.ErrAggregateCapability, 422, ""}, {errExecutionHTTPSize, 413, ""},
		{&payload.Error{Category: "incoming_record_invalid"}, 422, ""},
		{&payload.Error{Category: "stored_record_invalid"}, 500, ""},
		{context.DeadlineExceeded, 504, ""}, {mysql.ErrOwnerContractUnavailable, 503, ""},
		{&mysqlexecution.AdmissionOutcomeUnknown{ExecutionID: id, Cause: errors.New("secret")}, 503, id},
		{&ExecutionWaitError{ExecutionID: id, Cause: context.Canceled}, 503, id},
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		executionHTTPError(c, tc.err)
		if rec.Code != tc.status || strings.Contains(rec.Body.String(), "secret") {
			t.Fatal(tc.err, rec.Code, rec.Body.String())
		}
		if tc.id != "" && !strings.Contains(rec.Body.String(), tc.id) {
			t.Fatal("missing reconciliation ID")
		}
	}
}
