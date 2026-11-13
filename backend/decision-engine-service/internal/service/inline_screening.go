package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// One deadline bounds all screening rules for a decision, not each search.
const inlineScreeningTimeout = 2 * time.Second

func (s DecisionService) applyInlineScreening(ctx context.Context, item *decision.Decision, fields map[string]any, routing *ports.EvaluationRoutingSnapshot) ([]screening.Execution, error) {
	bounded, cancel := context.WithTimeout(ctx, inlineScreeningTimeout)
	defer cancel()
	executions, err := s.buildScreeningPhase(bounded, *item, fields, routing, true)
	if err != nil {
		return nil, err
	}
	for _, execution := range executions {
		var summary struct {
			ForcedOutcome decision.Outcome `json:"forced_outcome"`
		}
		if json.Unmarshal(execution.ResponseJSON, &summary) != nil {
			return nil, screening.ErrInlineFailed
		}
		if outcomeSeverity(summary.ForcedOutcome) > outcomeSeverity(item.Outcome) {
			item.Outcome = summary.ForcedOutcome
		}
	}
	return executions, nil
}

func outcomeSeverity(outcome decision.Outcome) int {
	switch outcome {
	case decision.OutcomeApprove:
		return 0
	case decision.OutcomeReview:
		return 1
	case decision.OutcomeBlockAndReview:
		return 2
	case decision.OutcomeDecline:
		return 3
	default:
		return -1
	}
}
