package dto

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type CreateTestRunRequest struct {
	PhantomIterationID string    `json:"phantom_iteration_id"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type TestRunResponse struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	ScenarioID         string    `json:"scenario_id"`
	LiveIterationID    string    `json:"live_iteration_id"`
	PhantomIterationID string    `json:"phantom_iteration_id"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func AdaptTestRun(t scenario.TestRun) TestRunResponse {
	return TestRunResponse{
		ID:                 t.ID,
		TenantID:           t.TenantID,
		ScenarioID:         t.ScenarioID,
		LiveIterationID:    t.LiveIterationID,
		PhantomIterationID: t.PhantomIterationID,
		Status:             string(t.Status),
		CreatedAt:          t.CreatedAt,
		ExpiresAt:          t.ExpiresAt,
		UpdatedAt:          t.UpdatedAt,
	}
}

type TestRunEvaluationResponse struct {
	Live    DecisionEvaluationResponse `json:"live"`
	Phantom DecisionEvaluationResponse `json:"phantom"`
}

func AdaptTestRunEvaluation(result service.TestRunEvaluationResult) TestRunEvaluationResponse {
	return TestRunEvaluationResponse{
		Live:    AdaptDecisionEvaluation(result.Live),
		Phantom: AdaptDecisionEvaluation(result.Phantom),
	}
}

type TestRunDecisionSummaryResponse struct {
	Outcome string `json:"outcome"`
	Score   int    `json:"score"`
	Count   int    `json:"count"`
}

func AdaptTestRunDecisionSummary(item service.TestRunDecisionSummary) TestRunDecisionSummaryResponse {
	return TestRunDecisionSummaryResponse{
		Outcome: item.Outcome,
		Score:   item.Score,
		Count:   item.Count,
	}
}

type TestRunRuleStatResponse struct {
	RuleID        string `json:"rule_id"`
	RuleName      string `json:"rule_name"`
	HitCount      int    `json:"hit_count"`
	NoHitCount    int    `json:"no_hit_count"`
	SnoozedCount  int    `json:"snoozed_count"`
	TotalCount    int    `json:"total_count"`
}

func AdaptTestRunRuleStat(item service.TestRunRuleStat) TestRunRuleStatResponse {
	return TestRunRuleStatResponse{
		RuleID:       item.RuleID,
		RuleName:     item.RuleName,
		HitCount:     item.HitCount,
		NoHitCount:   item.NoHitCount,
		SnoozedCount: item.SnoozedCount,
		TotalCount:   item.TotalCount,
	}
}

func AdaptPhantomDecision(item decision.PhantomDecision) DecisionResponse {
	return DecisionResponse{
		ID:                  item.ID,
		TenantID:            item.TenantID,
		ScenarioID:          item.ScenarioID,
		ScenarioIterationID: item.ScenarioIterationID,
		ObjectID:            item.ObjectID,
		ObjectType:          item.ObjectType,
		Outcome:             string(item.Outcome),
		Score:               item.Score,
		Triggered:           item.Triggered,
		CreatedAt:           item.CreatedAt,
	}
}
