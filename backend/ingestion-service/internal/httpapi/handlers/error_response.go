package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

type apiErrorEnvelope struct {
	Error apiErrorBody `json:"error"`
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

func writeBadRequest(c *gin.Context, message string, attrs ...any) {
	writeAPIError(c, apiErrorSpec{
		Status:   http.StatusBadRequest,
		Category: "invalid_request",
	}, "bad_parameter", message, nil, attrs...)
}

func writeServiceError(c *gin.Context, err error, attrs ...any) {
	spec, code, message := classifyServiceError(err)
	writeAPIError(c, spec, code, message, err, attrs...)
}

func writeAPIError(c *gin.Context, spec apiErrorSpec, code, message string, err error, attrs ...any) {
	c.Set("error_category", spec.Category)
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
	c.JSON(spec.Status, body)
}

func classifyServiceError(err error) (apiErrorSpec, string, string) {
	if err == nil {
		return apiErrorSpec{Status: http.StatusInternalServerError, Category: "internal_error"}, "internal_error", "request failed"
	}

	switch {
	case errors.Is(err, service.ErrIdempotencyKeyReused):
		return apiErrorSpec{Status: http.StatusConflict, Category: "idempotency_conflict"}, "idempotency_key_reused", "idempotency key was reused with a different payload"
	case errors.Is(err, context.DeadlineExceeded):
		return apiErrorSpec{Status: http.StatusGatewayTimeout, Category: "timeout"}, "request_timeout", "request timed out"
	case errors.Is(err, context.Canceled):
		return apiErrorSpec{Status: http.StatusRequestTimeout, Category: "canceled"}, "request_canceled", "request was canceled"
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "tenant is not writable for ingestion"):
		return apiErrorSpec{Status: http.StatusConflict, Category: "tenant_not_writable"}, "tenant_not_writable", "tenant is not writable for ingestion"
	case strings.Contains(message, "unexpected status from data-model-service"),
		strings.Contains(message, "data-model-service response missing revision_id"),
		strings.Contains(message, "perform request:"),
		strings.Contains(message, "decode response:"):
		return apiErrorSpec{Status: http.StatusBadGateway, Category: "dependency_failure"}, "dependency_failure", "data model dependency request failed"
	default:
		return apiErrorSpec{Status: http.StatusInternalServerError, Category: "internal_error"}, "internal_error", "request failed"
	}
}

func writeOverloaded(c *gin.Context, code, message string, attrs ...any) {
	writeAPIError(c, apiErrorSpec{
		Status:   http.StatusTooManyRequests,
		Category: "overloaded",
	}, code, message, nil, attrs...)
}
