package evalerrors

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestLocalTenantReadsAreNotUpstreamOutages(t *testing.T) {
	cases := []struct {
		err      error
		status   int
		category string
	}{
		{ports.ErrAggregateCapability, http.StatusUnprocessableEntity, "aggregate_pushdown_unsupported"},
		{ports.ErrPublishedModelUnavailable, http.StatusServiceUnavailable, "published_model_unavailable"},
		{&ports.LocalTenantReadError{Cause: errors.New("SQL execution failed")}, http.StatusInternalServerError, "internal_error"},
		{errors.New("unexpected status from ingestion-service: 503"), http.StatusBadGateway, "dependency_failure"},
	}
	for _, c := range cases {
		got := Classify(fmt.Errorf("aggregate pushdown failed: %w", c.err))
		if got.Status != c.status || got.Category != c.category {
			t.Fatal(got, c)
		}
	}
}
