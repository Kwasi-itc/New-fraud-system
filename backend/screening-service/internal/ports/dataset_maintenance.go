package ports

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"time"
)

type DatasetCheckpoint struct {
	Cursor                                string
	DeltaDone, NeedsRescreen, ObjectsDone bool
	Pages, Queued                         int
}

// Methods mutate only inside an enclosing transaction. Provider IO is separate.
type DatasetMaintenanceRepository interface {
	ClaimMaintenance(context.Context, string, string, string, time.Time) (DatasetCheckpoint, bool, error)
	SaveDeltaPage(context.Context, string, string, string, screening.DatasetDelta, time.Time) (DatasetCheckpoint, error)
	RequeueMaintenancePage(context.Context, string, string, string, time.Time) ([]string, bool, error)
	FinishMaintenance(context.Context, string, string, string, string, string, time.Time) error
	RecoverableMaintenance(context.Context, int) ([]screening.DatasetUpdateJob, error)
}
