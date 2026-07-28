package decision

import (
	"encoding/json"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

type Outcome string

const (
	OutcomeApprove        Outcome = "approve"
	OutcomeReview         Outcome = "review"
	OutcomeBlockAndReview Outcome = "block_and_review"
	OutcomeDecline        Outcome = "decline"
)

type Decision struct {
	ID                  string
	TenantID            string
	ScenarioID          string
	ScenarioIterationID string
	ObjectID            string
	ObjectType          string
	RequestBody         json.RawMessage
	Outcome             Outcome
	Score               int
	Triggered           bool
	CreatedAt           time.Time
}

type RuleExecution struct {
	ID            string
	DecisionID    string
	RuleID        string
	RuleName      string
	Outcome       string
	ScoreModifier int
	Evaluation    *domainast.EvaluationNode
	CreatedAt     time.Time
}
