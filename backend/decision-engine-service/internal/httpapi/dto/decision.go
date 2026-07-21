package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type EvaluateDecisionRequest struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
}

type CreateDecisionRequest struct {
	ScenarioID string         `json:"scenario_id"`
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
}

type IngestionTriggerRequest struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
	Source     string         `json:"source,omitempty"`
}

type DecisionResponse struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenant_id"`
	ScenarioID          string    `json:"scenario_id"`
	ScenarioIterationID string    `json:"scenario_iteration_id"`
	ObjectID            string    `json:"object_id"`
	ObjectType          string    `json:"object_type"`
	Outcome             string    `json:"outcome"`
	Score               int       `json:"score"`
	Triggered           bool      `json:"triggered"`
	CreatedAt           time.Time `json:"created_at"`
}

type DecisionDetailResponse struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"tenant_id"`
	ScenarioID          string          `json:"scenario_id"`
	ScenarioIterationID string          `json:"scenario_iteration_id"`
	ObjectID            string          `json:"object_id"`
	ObjectType          string          `json:"object_type"`
	RequestBody         json.RawMessage `json:"request_body"`
	Outcome             string          `json:"outcome"`
	Score               int             `json:"score"`
	Triggered           bool            `json:"triggered"`
	CreatedAt           time.Time       `json:"created_at"`
}

type PaginationResponse struct {
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	HasMore    bool `json:"has_more"`
	NextOffset *int `json:"next_offset,omitempty"`
}

type RuleExecutionResponse struct {
	ID            string    `json:"id"`
	DecisionID    string    `json:"decision_id"`
	RuleID        string    `json:"rule_id"`
	RuleName      string    `json:"rule_name"`
	Outcome       string    `json:"outcome"`
	ScoreModifier int       `json:"score_modifier"`
	CreatedAt     time.Time `json:"created_at"`
}

type DecisionEvaluationResponse struct {
	Triggered      bool                  `json:"triggered"`
	Decision       *DecisionResponse     `json:"decision,omitempty"`
	RuleExecutions []RuleExecutionResponse `json:"rule_executions,omitempty"`
}

type MultiDecisionEvaluationResponse struct {
	ObjectID string                     `json:"object_id"`
	Results  []DecisionEvaluationResponse `json:"results"`
}

type DecisionListEnvelope struct {
	Decisions  []DecisionResponse  `json:"decisions"`
	Pagination PaginationResponse `json:"pagination"`
}

func AdaptDecision(d decision.Decision) DecisionResponse {
	return DecisionResponse{
		ID:                  d.ID,
		TenantID:            d.TenantID,
		ScenarioID:          d.ScenarioID,
		ScenarioIterationID: d.ScenarioIterationID,
		ObjectID:            d.ObjectID,
		ObjectType:          d.ObjectType,
		Outcome:             string(d.Outcome),
		Score:               d.Score,
		Triggered:           d.Triggered,
		CreatedAt:           d.CreatedAt,
	}
}

func AdaptDecisionDetail(d decision.Decision) DecisionDetailResponse {
	return DecisionDetailResponse{
		ID:                  d.ID,
		TenantID:            d.TenantID,
		ScenarioID:          d.ScenarioID,
		ScenarioIterationID: d.ScenarioIterationID,
		ObjectID:            d.ObjectID,
		ObjectType:          d.ObjectType,
		RequestBody:         d.RequestBody,
		Outcome:             string(d.Outcome),
		Score:               d.Score,
		Triggered:           d.Triggered,
		CreatedAt:           d.CreatedAt,
	}
}

func AdaptRuleExecution(r decision.RuleExecution) RuleExecutionResponse {
	return RuleExecutionResponse{
		ID:            r.ID,
		DecisionID:    r.DecisionID,
		RuleID:        r.RuleID,
		RuleName:      r.RuleName,
		Outcome:       r.Outcome,
		ScoreModifier: r.ScoreModifier,
		CreatedAt:     r.CreatedAt,
	}
}

func AdaptDecisionEvaluation(result service.DecisionEvaluationResult) DecisionEvaluationResponse {
	out := DecisionEvaluationResponse{
		Triggered:      result.Triggered,
		RuleExecutions: make([]RuleExecutionResponse, len(result.RuleExecutions)),
	}
	if result.Decision != nil {
		d := AdaptDecision(*result.Decision)
		out.Decision = &d
	}
	for i, item := range result.RuleExecutions {
		out.RuleExecutions[i] = AdaptRuleExecution(item)
	}
	return out
}

func AdaptMultiDecisionEvaluation(result service.MultiScenarioEvaluationResult) MultiDecisionEvaluationResponse {
	out := MultiDecisionEvaluationResponse{
		ObjectID: result.ObjectID,
		Results:  make([]DecisionEvaluationResponse, len(result.Results)),
	}
	for i, item := range result.Results {
		out.Results[i] = AdaptDecisionEvaluation(item)
	}
	return out
}
