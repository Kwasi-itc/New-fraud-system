package handlers

import (
	"fmt"
	"net/http"
	"testing"

	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
)

func TestClassifyDecisionEvaluationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantStatus   int
		wantCategory string
	}{
		{
			name:         "aggregate pushdown overload",
			err:          fmt.Errorf("aggregate pushdown failed: %w", ingestionclient.StatusError{Service: "ingestion-service", StatusCode: http.StatusTooManyRequests}),
			wantStatus:   http.StatusTooManyRequests,
			wantCategory: "aggregate_pushdown_overloaded",
		},
		{
			name:         "dependency failure from ingestion",
			err:          fmt.Errorf("aggregate pushdown failed: unexpected status from ingestion-service: 503"),
			wantStatus:   http.StatusBadGateway,
			wantCategory: "dependency_failure",
		},
		{
			name:         "unsupported aggregate pushdown",
			err:          fmt.Errorf("aggregate pushdown unsupported: unsupported filter"),
			wantStatus:   http.StatusUnprocessableEntity,
			wantCategory: "aggregate_pushdown_unsupported",
		},
		{
			name:         "disabled broad read helper",
			err:          fmt.Errorf("related_records is disabled: broad record-list evaluation is not allowed"),
			wantStatus:   http.StatusUnprocessableEntity,
			wantCategory: "broad_read_helper_disabled",
		},
		{
			name:         "evaluation invalid",
			err:          fmt.Errorf("object_type does not match scenario trigger object type"),
			wantStatus:   http.StatusUnprocessableEntity,
			wantCategory: "evaluation_invalid",
		},
		{
			name:         "internal configuration",
			err:          fmt.Errorf("tenant data reader is not configured"),
			wantStatus:   http.StatusInternalServerError,
			wantCategory: "internal_configuration",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyDecisionEvaluationError(tt.err)
			if got.Status != tt.wantStatus || got.Category != tt.wantCategory {
				t.Fatalf("classifyDecisionEvaluationError(%v) = %+v, want status=%d category=%q", tt.err, got, tt.wantStatus, tt.wantCategory)
			}
		})
	}
}
