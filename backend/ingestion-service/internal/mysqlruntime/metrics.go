package mysqlruntime

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

// MetricsSnapshot is scoped to the configured tenant/table. Callers cannot
// broaden it through headers or query parameters. Current owner authority is
// checked before and inside the bounded database snapshot.
func (s Deferred) MetricsSnapshot(ctx context.Context) (ingestion.DeferredIngestMetrics, error) {
	w, err := s.work(ctx, s.Config.TenantID, s.Config.ObjectType)
	if err != nil {
		return ingestion.DeferredIngestMetrics{}, err
	}
	result, err := w.DeferredMetricsSnapshot(ctx, 10000)
	return result, boundary(err)
}
