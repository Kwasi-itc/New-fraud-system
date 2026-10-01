package casepkg

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type ReportContent struct {
	Title        string      `json:"title"`
	Subject      string      `json:"subject"`
	Narrative    string      `json:"narrative"`
	ActivityFrom *time.Time  `json:"activity_from,omitempty"`
	ActivityTo   *time.Time  `json:"activity_to,omitempty"`
	FileIDs      []uuid.UUID `json:"file_ids"`
}
type EvidenceReference struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
}
type ReportPayload struct {
	Format       string              `json:"format"`
	Content      ReportContent       `json:"content"`
	CaseSnapshot *Case               `json:"case_snapshot,omitempty"`
	Evidence     []EvidenceReference `json:"evidence,omitempty"`
}
type Report struct {
	LegacyPayload json.RawMessage `json:"legacy_payload,omitempty"`
	ID            uuid.UUID       `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	CaseID        uuid.UUID       `json:"case_id"`
	Status        string          `json:"status"`
	Version       int             `json:"version"`
	Payload       ReportPayload   `json:"payload"`
	CreatedBy     string          `json:"created_by"`
	CompletedBy   *string         `json:"completed_by,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	CompletedAt   *time.Time      `json:"completed_at,omitempty"`
}
type ReportPage struct {
	Reports    []Report `json:"reports"`
	NextCursor string   `json:"next_cursor,omitempty"`
}
