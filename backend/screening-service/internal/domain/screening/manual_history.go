package screening

import "time"

type ManualHistoryFilter struct {
	Status, EntityType  string
	From, Until, Before *time.Time
	BeforeID            string
	Limit               int
}
type ManualHistoryItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	EntityType string    `json:"entity_type"`
	Status     Status    `json:"status"`
	Partial    bool      `json:"partial"`
	CreatedAt  time.Time `json:"created_at"`
}
type ManualHistoryPage struct {
	Items      []ManualHistoryItem `json:"items"`
	NextCursor string              `json:"next_cursor"`
}
