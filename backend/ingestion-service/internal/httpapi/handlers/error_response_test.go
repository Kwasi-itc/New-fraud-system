package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

func TestClassifyServiceError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantStatus   int
		wantCategory string
		wantCode     string
	}{
		{
			name:         "idempotency conflict",
			err:          service.ErrIdempotencyKeyReused,
			wantStatus:   http.StatusConflict,
			wantCategory: "idempotency_conflict",
			wantCode:     "idempotency_key_reused",
		},
		{
			name:         "tenant not writable",
			err:          errors.New("tenant is not writable for ingestion"),
			wantStatus:   http.StatusConflict,
			wantCategory: "tenant_not_writable",
			wantCode:     "tenant_not_writable",
		},
		{
			name:         "dependency failure",
			err:          errors.New("unexpected status from data-model-service: 503"),
			wantStatus:   http.StatusBadGateway,
			wantCategory: "dependency_failure",
			wantCode:     "dependency_failure",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, code, _ := classifyServiceError(tt.err)
			if spec.Status != tt.wantStatus || spec.Category != tt.wantCategory || code != tt.wantCode {
				t.Fatalf("classifyServiceError(%v) = spec=%+v code=%q, want status=%d category=%q code=%q", tt.err, spec, code, tt.wantStatus, tt.wantCategory, tt.wantCode)
			}
		})
	}
}
