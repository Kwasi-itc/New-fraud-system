package casepkg

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type ExplorationHop struct {
	Connection string `json:"connection"`
	ObjectID   string `json:"object_id"`
}
type ExplorationRequest struct {
	DecisionID    uuid.UUID        `json:"decision_id"`
	Path          []ExplorationHop `json:"path"`
	Connection    string           `json:"connection"`
	After         string           `json:"after"`
	DecisionAfter string           `json:"decision_after"`
}
type ExplorationRecord struct {
	ObjectID string          `json:"object_id"`
	Data     json.RawMessage `json:"data"`
}
type ExplorationConnection struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}
type Exploration struct {
	Table        string                  `json:"table"`
	Current      ExplorationRecord       `json:"current"`
	Connections  []ExplorationConnection `json:"connections"`
	Records      []ExplorationRecord     `json:"records"`
	Next         string                  `json:"next_after"`
	Decisions    []ExplorationDecision   `json:"decisions"`
	NextDecision string                  `json:"next_decision_after"`
}

type ExplorationCase struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}
type ExplorationDecision struct {
	ID           uuid.UUID        `json:"id"`
	ScenarioID   uuid.UUID        `json:"scenario_id"`
	ScenarioName string           `json:"scenario_name"`
	Outcome      string           `json:"outcome"`
	Score        int              `json:"score"`
	Triggered    bool             `json:"triggered"`
	CreatedAt    time.Time        `json:"created_at"`
	Case         *ExplorationCase `json:"case,omitempty"`
	CanAttach    bool             `json:"can_attach"`
}
type ExplorationAttach struct {
	ExplorationRequest
	TargetDecisionID uuid.UUID `json:"target_decision_id"`
}
