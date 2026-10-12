package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/evalerrors"
)

type apiErrorEnvelope struct {
	Error            apiErrorBody    `json:"error"`
	ValidationErrors []payload.Issue `json:"validation_errors,omitempty"`
	ModelRevision    string          `json:"model_revision,omitempty"`
	Truncated        bool            `json:"truncated,omitempty"`
}

type apiErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Category  string `json:"category,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Details   string `json:"details,omitempty"`
}

type apiErrorSpec struct {
	Status   int
	Category string
}

func writeBadRequestError(c *gin.Context, code, message string, err error, attrs ...any) {
	writeAPIError(c, apiErrorSpec{
		Status:   http.StatusBadRequest,
		Category: "invalid_request",
	}, code, message, err, attrs...)
}

func writeDecisionEvaluationError(c *gin.Context, code, message string, err error, attrs ...any) {
	writeAPIError(c, classifyDecisionEvaluationError(err), code, message, err, attrs...)
}

func writeAPIError(c *gin.Context, spec apiErrorSpec, code, message string, err error, attrs ...any) {
	attrs = append(attrs, "error_code", code, "error_category", spec.Category, "http_status", spec.Status)
	logHandlerFailure(c, message, err, attrs...)

	body := apiErrorEnvelope{
		Error: apiErrorBody{
			Code:      code,
			Message:   message,
			Category:  spec.Category,
			RequestID: requestIDFromContext(c),
		},
	}
	if err != nil && spec.Status < http.StatusInternalServerError {
		body.Error.Details = err.Error()
	}
	var validation *payload.Error
	if errors.As(err, &validation) {
		body.ModelRevision = validation.ModelRevision
		if validation.Category != "stored_record_invalid" {
			body.ValidationErrors, body.Truncated = validation.Issues, validation.Truncated
		}
	}
	c.JSON(spec.Status, body)
}

// bindEvaluationJSON preserves numeric text for full-object validation and rejects trailing bodies.
func bindEvaluationJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("request must contain one JSON object")
	}
	return nil
}

func classifyDecisionEvaluationError(err error) apiErrorSpec {
	classification := evalerrors.Classify(err)
	return apiErrorSpec{Status: classification.Status, Category: classification.Category}
}
