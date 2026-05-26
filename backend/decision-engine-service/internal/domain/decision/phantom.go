package decision

import "time"

type PhantomDecision struct {
	ID                  string
	TestRunID           string
	TenantID            string
	ScenarioID          string
	ScenarioIterationID string
	ObjectID            string
	ObjectType          string
	Outcome             Outcome
	Score               int
	Triggered           bool
	CreatedAt           time.Time
}

type PhantomRuleExecution struct {
	ID                string
	PhantomDecisionID string
	RuleID            string
	RuleName          string
	Outcome           string
	ScoreModifier     int
	CreatedAt         time.Time
}
