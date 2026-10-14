package casepkg

import "github.com/google/uuid"

// Unavailable deliberately does not distinguish missing and private resources.
type DecisionSelection struct {
	DecisionID uuid.UUID  `json:"decision_id"`
	State      string     `json:"state"`
	CaseID     *uuid.UUID `json:"case_id,omitempty"`
	CaseName   *string    `json:"case_name,omitempty"`
	CaseStatus *string    `json:"case_status,omitempty"`
}
