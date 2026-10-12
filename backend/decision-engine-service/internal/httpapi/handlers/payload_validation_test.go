package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/gin-gonic/gin"
)

func TestPayloadValidationEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	writeDecisionEvaluationError(c, "evaluate_scenario_failed", "evaluation failed", &payload.Error{Category: "payload_validation_failed", ModelRevision: "rev-1", Issues: []payload.Issue{{Field: "amount", Code: "missing_required", Message: "required"}}})
	if w.Code != 422 {
		t.Fatalf("status %d", w.Code)
	}
	var body apiErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Category != "payload_validation_failed" || body.ModelRevision != "rev-1" || body.ValidationErrors[0].Field != "amount" {
		t.Fatalf("%s", w.Body.String())
	}
}

func TestStoredRecordValidationDetailsAreHidden(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	writeDecisionEvaluationError(c, "evaluate_scenario_failed", "evaluation failed", &payload.Error{Category: "stored_record_invalid", Issues: []payload.Issue{{Field: "private", Code: "missing_required", Message: "required"}}})
	if w.Code != 500 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestEvaluationBindingPreservesIntegersAndRejectsTrailingJSON(t *testing.T) {
	for _, suffix := range []string{"", ` {}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"fields":{"count":9223372036854775807}}`+suffix))
		var req dto.EvaluateDecisionRequest
		err := bindEvaluationJSON(c, &req)
		if suffix != "" {
			if err == nil {
				t.Fatal("trailing body accepted")
			}
			continue
		}
		if err != nil || req.Fields["count"] != json.Number("9223372036854775807") {
			t.Fatalf("%v %#v", err, req)
		}
	}
}
