package evalerrors

import (
	"net/http"
	"strings"

	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
)

type Classification struct {
	Status   int
	Category string
}

func Classify(err error) Classification {
	if err == nil {
		return Classification{Status: http.StatusInternalServerError, Category: "internal_error"}
	}
	if isAggregatePushdownOverload(err) {
		return Classification{Status: http.StatusTooManyRequests, Category: "aggregate_pushdown_overloaded"}
	}
	message := strings.ToLower(err.Error())

	switch {
	case strings.Contains(message, "related_count is disabled:"),
		strings.Contains(message, "related_records is disabled:"):
		return Classification{Status: http.StatusUnprocessableEntity, Category: "broad_read_helper_disabled"}
	case strings.Contains(message, "unexpected status from ingestion-service"),
		strings.Contains(message, "unexpected status from data-model-service"),
		strings.Contains(message, "data-model-service response missing revision_id"),
		strings.Contains(message, "aggregate pushdown failed:"),
		strings.Contains(message, "perform request:"),
		strings.Contains(message, "decode response:"):
		return Classification{Status: http.StatusBadGateway, Category: "dependency_failure"}
	case strings.Contains(message, "aggregate pushdown unsupported:"):
		return Classification{Status: http.StatusUnprocessableEntity, Category: "aggregate_pushdown_unsupported"}
	case strings.Contains(message, "not found"):
		return Classification{Status: http.StatusNotFound, Category: "not_found"}
	case strings.Contains(message, "tenant data reader is not configured"),
		strings.Contains(message, "tenant model is not configured"),
		strings.Contains(message, "repository is not configured"),
		strings.Contains(message, "unexpected scenario cache load type"),
		strings.Contains(message, "unexpected iteration cache load type"),
		strings.Contains(message, "unexpected rules cache load type"),
		strings.Contains(message, "unexpected tenant model cache load type"),
		strings.Contains(message, "unexpected workflow rules cache load type"),
		strings.Contains(message, "unexpected workflows cache load type"),
		strings.Contains(message, "unexpected screening configs cache load type"),
		strings.Contains(message, "unexpected scoring configs cache load type"),
		strings.Contains(message, "unexpected live scenarios cache load type"):
		return Classification{Status: http.StatusInternalServerError, Category: "internal_configuration"}
	case strings.Contains(message, "scenario has no live iteration"),
		strings.Contains(message, "object_type does not match scenario trigger object type"),
		strings.Contains(message, "formula did not evaluate to boolean"),
		strings.Contains(message, "unsupported"),
		strings.Contains(message, "invalid"),
		strings.Contains(message, "expects"),
		strings.Contains(message, "must "),
		strings.Contains(message, "missing"),
		strings.Contains(message, "not found in tenant model"):
		return Classification{Status: http.StatusUnprocessableEntity, Category: "evaluation_invalid"}
	default:
		return Classification{Status: http.StatusInternalServerError, Category: "internal_error"}
	}
}

func isAggregatePushdownOverload(err error) bool {
	if err == nil {
		return false
	}
	if !strings.Contains(strings.ToLower(err.Error()), "aggregate pushdown failed:") {
		return false
	}
	return ingestionclient.IsStatusError(err, "ingestion-service", http.StatusTooManyRequests)
}
