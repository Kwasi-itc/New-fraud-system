package service

import (
	"context"
	"encoding/json"
	"fmt"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type RuleValidationResult struct {
	RuleID string   `json:"rule_id"`
	Name   string   `json:"name"`
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type IterationValidationResult struct {
	Valid         bool                   `json:"valid"`
	ModelRevision string                 `json:"model_revision"`
	TriggerErrors []string               `json:"trigger_errors"`
	RuleResults   []RuleValidationResult `json:"rule_results"`
	Errors        []string               `json:"errors"`
}

type IterationValidator interface {
	ValidateIteration(ctx context.Context, tenantID, scenarioID, iterationID string) (IterationValidationResult, error)
}

type ValidationService struct {
	dataModelReader ports.DataModelReader
	scenarioRepo    ports.ScenarioRepository
	iterationRepo   ports.ScenarioIterationRepository
	ruleRepo        ports.RuleRepository
}

func NewValidationService(
	dataModelReader ports.DataModelReader,
	scenarioRepo ports.ScenarioRepository,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
) ValidationService {
	return ValidationService{
		dataModelReader: dataModelReader,
		scenarioRepo:    scenarioRepo,
		iterationRepo:   iterationRepo,
		ruleRepo:        ruleRepo,
	}
}

func (s ValidationService) ValidateIteration(ctx context.Context, tenantID, scenarioID, iterationID string) (IterationValidationResult, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return IterationValidationResult{}, err
	}
	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return IterationValidationResult{}, err
	}
	rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return IterationValidationResult{}, err
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return IterationValidationResult{}, err
	}
	return validateIterationSnapshot(scn, iteration, rules, model)
}

// ValidateIterationSnapshot provides the same detailed validation as the source
// service, with explicit target work budgets and relationship checks. The result
// describes these inputs only; it grants no authority to commit or publish them.
func ValidateIterationSnapshot(parent scenario.Scenario, iteration scenario.Iteration, rules []scenario.Rule, model ports.TenantModel, limits ExecutionPreparationLimits) (IterationValidationResult, error) {
	if err := limits.Validate(); err != nil {
		return IterationValidationResult{}, err
	}
	if parent.ID == "" || parent.TenantID == "" || iteration.ID == "" || iteration.TenantID != parent.TenantID || iteration.ScenarioID != parent.ID || model.RevisionID == "" {
		return IterationValidationResult{}, ErrExecutionPreparation
	}
	if len(rules) > limits.MaxItems {
		return IterationValidationResult{}, ErrExecutionPreparationLimit
	}
	raw, err := json.Marshal(struct {
		Parent    scenario.Scenario
		Iteration scenario.Iteration
		Rules     []scenario.Rule
		Model     ports.TenantModel
	}{parent, iteration, rules, model})
	if err != nil {
		return IterationValidationResult{}, ErrExecutionPreparation
	}
	if len(raw) > limits.MaxBytes {
		return IterationValidationResult{}, ErrExecutionPreparationLimit
	}
	nodes := 0
	var visit func(domainast.Node, int) error
	visit = func(node domainast.Node, depth int) error {
		nodes++
		if nodes > 10000 || depth > 64 {
			return ErrExecutionPreparationLimit
		}
		for _, child := range node.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		for _, child := range node.NamedChildren {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	formulas := []json.RawMessage{iteration.TriggerFormula}
	seen := map[string]bool{}
	for _, rule := range rules {
		if rule.ID == "" || seen[rule.ID] || rule.TenantID != parent.TenantID || rule.IterationID != iteration.ID {
			return IterationValidationResult{}, ErrExecutionPreparation
		}
		seen[rule.ID] = true
		formulas = append(formulas, rule.Formula)
	}
	for _, formula := range formulas {
		var node domainast.Node
		if json.Unmarshal(formula, &node) == nil {
			if err := visit(node, 1); err != nil {
				return IterationValidationResult{}, err
			}
		}
	}
	result, err := validateIterationSnapshot(parent, iteration, rules, model)
	if err != nil {
		return IterationValidationResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return IterationValidationResult{}, err
	}
	if len(encoded) > limits.MaxBytes {
		return IterationValidationResult{}, ErrExecutionPreparationLimit
	}
	return result, nil
}

func validateIterationSnapshot(scn scenario.Scenario, iteration scenario.Iteration, rules []scenario.Rule, model ports.TenantModel) (IterationValidationResult, error) {
	result := IterationValidationResult{Valid: true}
	result.ModelRevision = model.RevisionID

	if table, ok := model.Tables[scn.TriggerObjectType]; !ok || table.Archived {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("trigger object type %q not found in tenant model", scn.TriggerObjectType))
		return result, nil
	}

	if len(iteration.TriggerFormula) == 0 {
		result.Valid = false
		result.TriggerErrors = append(result.TriggerErrors, "trigger formula is required")
	} else {
		var triggerNode domainast.Node
		if err := json.Unmarshal(iteration.TriggerFormula, &triggerNode); err != nil {
			result.Valid = false
			result.TriggerErrors = append(result.TriggerErrors, "trigger formula is not valid JSON AST")
		} else {
			valueType, errs := asteval.ValidateNode(triggerNode, model, scn.TriggerObjectType)
			if len(errs) > 0 {
				result.Valid = false
				result.TriggerErrors = append(result.TriggerErrors, errs...)
			}
			if valueType != domainast.ValueTypeBool {
				result.Valid = false
				result.TriggerErrors = append(result.TriggerErrors, "trigger formula must return a boolean")
			}
		}
	}

	if iteration.ScoreReviewThreshold == nil ||
		iteration.ScoreBlockAndReviewThreshold == nil ||
		iteration.ScoreDeclineThreshold == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "all score thresholds are required")
	} else if *iteration.ScoreBlockAndReviewThreshold < *iteration.ScoreReviewThreshold ||
		*iteration.ScoreDeclineThreshold < *iteration.ScoreBlockAndReviewThreshold {
		result.Valid = false
		result.Errors = append(result.Errors, "thresholds must satisfy review <= block_and_review <= decline")
	}

	for _, rule := range rules {
		ruleResult := RuleValidationResult{
			RuleID: rule.ID,
			Name:   rule.Name,
			Valid:  true,
		}

		var node domainast.Node
		if len(rule.Formula) == 0 {
			ruleResult.Valid = false
			ruleResult.Errors = append(ruleResult.Errors, "rule formula is required")
		} else if err := json.Unmarshal(rule.Formula, &node); err != nil {
			ruleResult.Valid = false
			ruleResult.Errors = append(ruleResult.Errors, "rule formula is not valid JSON AST")
		} else {
			valueType, errs := asteval.ValidateNode(node, model, scn.TriggerObjectType)
			if len(errs) > 0 {
				ruleResult.Valid = false
				ruleResult.Errors = append(ruleResult.Errors, errs...)
			}
			if valueType != domainast.ValueTypeBool {
				ruleResult.Valid = false
				ruleResult.Errors = append(ruleResult.Errors, "rule formula must return a boolean")
			}
		}

		if !ruleResult.Valid {
			result.Valid = false
		}
		result.RuleResults = append(result.RuleResults, ruleResult)
	}

	return result, nil
}
