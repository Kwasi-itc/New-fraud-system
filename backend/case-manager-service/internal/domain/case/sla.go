package casepkg

import "time"

// SLA days are fixed 24-hour UTC days from creation, using the current inbox's
// current policy. Moving or editing the SLA changes the derived deadline.
func SLADueAt(created time.Time, days *int) *time.Time {
	if days == nil {
		return nil
	}
	due := created.UTC().Add(time.Duration(*days) * 24 * time.Hour)
	return &due
}
