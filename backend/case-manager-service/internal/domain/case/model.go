package casepkg

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending       Status = "pending"
	StatusInvestigating Status = "investigating"
	StatusClosed        Status = "closed"
)

type Outcome string

const (
	OutcomeUnset         Outcome = "unset"
	OutcomeFalsePositive Outcome = "false_positive"
	OutcomeValuableAlert Outcome = "valuable_alert"
	OutcomeConfirmedRisk Outcome = "confirmed_risk"
)

type Type string

const (
	TypeDecision            Type = "decision"
	TypeContinuousScreening Type = "continuous_screening"
)

type Inbox struct {
	ID                      uuid.UUID  `json:"id"`
	TenantID                uuid.UUID  `json:"tenant_id"`
	Name                    string     `json:"name"`
	Status                  string     `json:"status"`
	EscalationInboxID       *uuid.UUID `json:"escalation_inbox_id,omitempty"`
	AutoAssignEnabled       bool       `json:"auto_assign_enabled"`
	CaseReviewManual        bool       `json:"case_review_manual"`
	CaseReviewOnCaseCreated bool       `json:"case_review_on_case_created"`
	CaseReviewOnEscalate    bool       `json:"case_review_on_escalate"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type Case struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	InboxID      uuid.UUID       `json:"inbox_id"`
	Name         string          `json:"name"`
	Status       Status          `json:"status"`
	Outcome      Outcome         `json:"outcome"`
	Type         Type            `json:"type"`
	AssignedTo   *string         `json:"assigned_to,omitempty"`
	SnoozedUntil *time.Time      `json:"snoozed_until,omitempty"`
	BoostReason  *string         `json:"boost_reason,omitempty"`
	ReviewLevel  *string         `json:"review_level,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Decisions    []DecisionLink  `json:"decisions,omitempty"`
	Screenings   []ScreeningLink `json:"screenings,omitempty"`
	Tags         []Tag           `json:"tags,omitempty"`
	Files        []File          `json:"files,omitempty"`
	Events       []Event         `json:"events,omitempty"`
}

type DecisionLink struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	CaseID     uuid.UUID  `json:"case_id"`
	DecisionID uuid.UUID  `json:"decision_id"`
	ScenarioID *uuid.UUID `json:"scenario_id,omitempty"`
	ObjectType string     `json:"object_type"`
	ObjectID   string     `json:"object_id"`
	PivotValue *string    `json:"pivot_value,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ScreeningLink struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	CaseID      uuid.UUID `json:"case_id"`
	ScreeningID uuid.UUID `json:"screening_id"`
	MatchID     *string   `json:"match_id,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type Tag struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	Target    string     `json:"target"`
	Name      string     `json:"name"`
	Color     string     `json:"color"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type File struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	CaseID      uuid.UUID `json:"case_id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	FileSize    int64     `json:"file_size"`
	StorageKey  string    `json:"storage_key"`
	UploadedBy  string    `json:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type Event struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	CaseID         uuid.UUID `json:"case_id"`
	UserID         *string   `json:"user_id,omitempty"`
	EventType      string    `json:"event_type"`
	AdditionalNote string    `json:"additional_note"`
	ResourceID     string    `json:"resource_id"`
	ResourceType   string    `json:"resource_type"`
	NewValue       string    `json:"new_value"`
	PreviousValue  string    `json:"previous_value"`
	CreatedAt      time.Time `json:"created_at"`
}

type CaseFilters struct {
	Statuses       []Status
	InboxIDs       []uuid.UUID
	Name           string
	IncludeSnoozed bool
	AssigneeID     string
}

func ValidateStatusTransition(current, next Status) error {
	if current == next {
		return nil
	}
	switch current {
	case StatusPending:
		return nil
	case StatusInvestigating:
		if next == StatusClosed {
			return nil
		}
	case StatusClosed:
		if next == StatusInvestigating {
			return nil
		}
	}
	return fmt.Errorf("invalid case status transition from %s to %s", current, next)
}

func ValidReviewLevel(level string) bool {
	return level == "probable_false_positive" || level == "investigate" || level == "escalate"
}
