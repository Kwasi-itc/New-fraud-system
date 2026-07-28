package service

import (
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

func outcomeFromScore(score int, iteration scenario.Iteration) decision.Outcome {
	if iteration.ScoreDeclineThreshold != nil && score >= *iteration.ScoreDeclineThreshold {
		return decision.OutcomeDecline
	}
	if iteration.ScoreBlockAndReviewThreshold != nil && score >= *iteration.ScoreBlockAndReviewThreshold {
		return decision.OutcomeBlockAndReview
	}
	if iteration.ScoreReviewThreshold != nil && score >= *iteration.ScoreReviewThreshold {
		return decision.OutcomeReview
	}
	return decision.OutcomeApprove
}

func newRuleExecution(
	now time.Time,
	decisionID string,
	rule scenario.Rule,
	matched bool,
	evaluation *domainast.EvaluationNode,
) decision.RuleExecution {
	outcome := "no_hit"
	if matched {
		outcome = "hit"
	} else {
		evaluation = nil
	}
	return decision.RuleExecution{
		ID:            "",
		DecisionID:    decisionID,
		RuleID:        rule.ID,
		RuleName:      rule.Name,
		Outcome:       outcome,
		ScoreModifier: rule.ScoreModifier,
		Evaluation:    evaluation,
		CreatedAt:     now,
	}
}
