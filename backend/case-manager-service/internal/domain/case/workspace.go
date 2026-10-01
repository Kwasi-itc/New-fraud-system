package casepkg

import (
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Cursor struct {
	ID      uuid.UUID `json:"id"`
	At      time.Time `json:"at"`
	Boosted bool      `json:"boosted"`
}

func ParseCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, Invalid("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, Invalid("invalid cursor")
	}
	var c Cursor
	if json.Unmarshal(data, &c) != nil || c.ID == uuid.Nil || c.At.IsZero() {
		return nil, Invalid("invalid cursor")
	}
	return &c, nil
}
func (c Cursor) Encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

type WorkspaceFilters struct {
	Overdue bool
	CaseFilters
	Before                 *Cursor
	CreatedFrom, CreatedTo *time.Time
	TagID                  *uuid.UUID
	Unassigned             bool
	ReviewLevel            string
	RelatedTo              *uuid.UUID
}
type QueuePage struct {
	Cases      []Case           `json:"cases"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Counts     map[string]int64 `json:"counts"`
}
type LinkPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
