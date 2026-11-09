package mysqlruntime

import (
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
)

func TestMetricsFailureBoundaries(t *testing.T) {
	if !errors.Is(boundary(store.ErrWorkMetricsBound), service.ErrRequestLimits) {
		t.Fatal("overflow must reject the whole request")
	}
	if !errors.Is(boundary(store.ErrWorkMetricsIntegrity), service.ErrUnavailable) {
		t.Fatal("corrupt lineage must be unavailable, not zero or missing")
	}
}
