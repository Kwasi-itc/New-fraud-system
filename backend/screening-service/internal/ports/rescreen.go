package ports

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

// ResumableDatasetJobs is optional; the native MySQL adapter advances a bounded
// page and its checkpoint atomically. Provider calls belong outside that scope.
type RescreenPlan struct {
	Delta      json.RawMessage `json:"delta,omitempty"`
	NextCursor string          `json:"next_cursor"`
	Changed    bool            `json:"changed"`
}
type RescreenProgress struct {
	Plan      RescreenPlan
	Requeued  int64
	Completed bool
}
type ResumableDatasetJobs interface {
	RescreenProgress(context.Context, string, string) (*RescreenProgress, error)
	AdvanceRescreen(context.Context, screening.DatasetUpdateJob, RescreenPlan, int) (RescreenProgress, error)
}
