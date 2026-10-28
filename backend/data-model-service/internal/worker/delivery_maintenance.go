package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

// DeliveryMaintenance runs two independently paginated streams. One instance
// uses at most one database call at a time. Configuration bounds work per cycle,
// rather than draining an arbitrarily growing queue before yielding.
type DeliveryMaintenance struct {
	pageSize, pagesPerStream int
	interval, pageTimeout    time.Duration
}

func NewDeliveryMaintenance(pageSize, pagesPerStream int, interval, pageTimeout time.Duration) (*DeliveryMaintenance, error) {
	if pageSize < 1 || pageSize > 1000 || pagesPerStream < 1 || pagesPerStream > 1000 || interval <= 0 || pageTimeout <= 0 {
		return nil, errors.New("maintenance requires page size/pages per stream 1..1000 and positive interval/timeout")
	}
	return &DeliveryMaintenance{pageSize, pagesPerStream, interval, pageTimeout}, nil
}

// Run retains cursors across paced cycles and wraps only after exhaustion.
// Failures terminate with stream and acknowledged-change context; the caller
// must surface them. It does not retry ambiguous commits or infer rollback.
// Store callbacks must honor cancellation. Run returns only after they exit.
// Cursors are local to a run, so restarting safely rescans indexed due work.
func (m *DeliveryMaintenance) Run(ctx context.Context, store ports.DeliveryMaintenance) error {
	if m == nil || m.pageSize < 1 || m.pagesPerStream < 1 || m.interval <= 0 || m.pageTimeout <= 0 || store == nil {
		return errors.New("configured maintenance scheduler and store required")
	}
	var cursors [2]*ports.DeliveryMaintenanceCursor
	for {
		for page := 0; page < m.pagesPerStream; page++ {
			for stream := range cursors {
				if err := ctx.Err(); err != nil {
					return context.Cause(ctx)
				}
				// An exhausted stream is visited once per cycle, not repeatedly.
				if page > 0 && cursors[stream] == nil {
					continue
				}
				call, cancel := context.WithTimeout(ctx, m.pageTimeout)
				n, next, err := store.MaintainPage(call, m.pageSize, stream == 1, cursors[stream])
				err = errors.Join(err, call.Err())
				cancel()
				if err != nil {
					return fmt.Errorf("delivery maintenance cleanup=%t acknowledged_changes=%d (additional commit may be uncertain): %w", stream == 1, n, err)
				}
				cursors[stream] = next
			}
		}
		timer := time.NewTimer(m.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
}
