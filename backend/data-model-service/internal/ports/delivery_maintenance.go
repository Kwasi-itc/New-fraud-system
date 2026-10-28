package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DeliveryMaintenanceCursor is an internal scan position, never authorization.
// Expiry and retention scans must keep independent cursors, including across
// pages that change no rows because every candidate is locked.
type DeliveryMaintenanceCursor struct {
	Queue   string
	Cleanup bool
	At      time.Time
	ID      uuid.UUID
}

type DeliveryMaintenance interface {
	// MaintainPage commits each candidate independently. On error count includes
	// only acknowledged commits; an ambiguous commit may have changed another
	// row. A nil cursor on success means the scan is exhausted.
	MaintainPage(context.Context, int, bool, *DeliveryMaintenanceCursor) (int, *DeliveryMaintenanceCursor, error)
}
