package dto

import "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"

type RuleValidationResultResponse struct {
	RuleID string   `json:"rule_id"`
	Name   string   `json:"name"`
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type IterationValidationResponse struct {
	Valid         bool                           `json:"valid"`
	ModelRevision string                         `json:"model_revision"`
	TriggerErrors []string                       `json:"trigger_errors"`
	RuleResults   []RuleValidationResultResponse `json:"rule_results"`
	Errors        []string                       `json:"errors"`
}

func AdaptIterationValidation(result service.IterationValidationResult) IterationValidationResponse {
	out := IterationValidationResponse{
		Valid:         result.Valid,
		ModelRevision: result.ModelRevision,
		TriggerErrors: append([]string(nil), result.TriggerErrors...),
		Errors:        append([]string(nil), result.Errors...),
		RuleResults:   make([]RuleValidationResultResponse, len(result.RuleResults)),
	}
	for i, item := range result.RuleResults {
		out.RuleResults[i] = RuleValidationResultResponse{
			RuleID: item.RuleID,
			Name:   item.Name,
			Valid:  item.Valid,
			Errors: append([]string(nil), item.Errors...),
		}
	}
	return out
}
