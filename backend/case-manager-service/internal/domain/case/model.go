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
	SLADays                 *int       `json:"sla_days"`
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
	SLADueAt     *time.Time      `json:"sla_due_at,omitempty"`
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
	Contributors []Contributor   `json:"contributors,omitempty"`
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
	SourceFileID *uuid.UUID `json:"source_file_id,omitempty"`
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	CaseID       uuid.UUID  `json:"case_id"`
	FileName     string     `json:"file_name"`
	ContentType  string     `json:"content_type"`
	FileSize     int64      `json:"file_size"`
	StorageKey   string     `json:"storage_key"`
	UploadedBy   string     `json:"uploaded_by"`
	CreatedAt    time.Time  `json:"created_at"`
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
	// AccessUserID is server-owned and never accepted from a request.
	AccessUserID   string
	Statuses       []Status
	InboxIDs       []uuid.UUID
	Name           string
	IncludeSnoozed bool
	AssigneeID     string
}

type InboxUser struct {
	Capacity          int       `json:"capacity"`
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	InboxID           uuid.UUID `json:"inbox_id"`
	UserID            string    `json:"user_id"`
	AutoAssignEnabled bool      `json:"auto_assign_enabled"`
	CreatedAt         time.Time `json:"created_at"`
}

type Contributor struct {
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type DecisionReference struct {
	ID         uuid.UUID
	ScenarioID uuid.UUID
	ObjectType string
	ObjectID   string
}

type ScreeningReference struct {
	MatchStatus   string
	ID            uuid.UUID
	DecisionID    *uuid.UUID
	ReviewInboxID *uuid.UUID
}

func ValidateStatusTransition(current, next Status) error {
	if !ValidStatus(current) || !ValidStatus(next) {
		return Invalid("invalid case status")
	}
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
	return Invalid(fmt.Sprintf("invalid case status transition from %s to %s", current, next))
}

func ValidReviewLevel(level string) bool {
	return level == "probable_false_positive" || level == "investigate" || level == "escalate"
}
