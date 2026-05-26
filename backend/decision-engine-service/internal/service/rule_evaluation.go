package service

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"golang.org/x/sync/errgroup"
)

const defaultRuleEvaluationConcurrency = 8

type evaluatedRule struct {
	Rule    scenario.Rule
	Matched bool
	Snoozed bool
}

func evaluateRules(
	ctx context.Context,
	rules []scenario.Rule,
	runtimeCtx asteval.Runtime,
	activeSnoozeGroups map[string]struct{},
	concurrency int,
) ([]evaluatedRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	results := make([]evaluatedRule, len(rules))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(resolveRuleEvaluationConcurrency(concurrency, len(rules)))

	for i, rule := range rules {
		i := i
		rule := rule
		group.Go(func() error {
			result := evaluatedRule{Rule: rule}
			if rule.SnoozeGroupID != nil {
				if _, ok := activeSnoozeGroups[*rule.SnoozeGroupID]; ok {
					result.Snoozed = true
					results[i] = result
					return nil
				}
			}

			matched, err := asteval.EvaluateFormula(groupCtx, rule.Formula, runtimeCtx)
			if err != nil {
				return fmt.Errorf("evaluate rule %q: %w", rule.Name, err)
			}

			result.Matched = matched
			results[i] = result
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func resolveRuleEvaluationConcurrency(configured, ruleCount int) int {
	if ruleCount <= 0 {
		return 1
	}
	if configured > 0 {
		if configured > ruleCount {
			return ruleCount
		}
		return configured
	}
	limit := defaultRuleEvaluationConcurrency
	if cpuCount := runtime.GOMAXPROCS(0); cpuCount > 0 && cpuCount < limit {
		limit = cpuCount
	}
	if limit > ruleCount {
		limit = ruleCount
	}
	if limit < 1 {
		return 1
	}
	return limit
}
