package ast_eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
)

func EvaluateFormulaWithEvidence(ctx context.Context, formula json.RawMessage, runtime Runtime) (bool, *domainast.EvaluationNode, error) {
	var node domainast.Node
	if err := json.Unmarshal(formula, &node); err != nil {
		return false, nil, err
	}
	evaluation, err := evaluateNodeWithEvidence(ctx, node, runtime)
	if err != nil {
		if errors.Is(err, sharedeventstore.ErrAggregationSkipped) {
			return false, &domainast.EvaluationNode{Function: node.Function, Constant: node.Constant, ReturnValue: false}, nil
		}
		return false, nil, err
	}
	boolResult, ok := evaluation.ReturnValue.(bool)
	if !ok {
		return false, nil, fmt.Errorf("formula did not evaluate to boolean")
	}
	return boolResult, &evaluation, nil
}

func evaluateNodeWithEvidence(ctx context.Context, node domainast.Node, runtime Runtime) (domainast.EvaluationNode, error) {
	evaluation := domainast.EvaluationNode{
		Function: node.Function,
		Constant: node.Constant,
	}
	if node.Function == "" || strings.EqualFold(node.Function, "constant") {
		evaluation.ReturnValue = node.Constant
		return evaluation, nil
	}

	switch canonicalFunctionName(node.Function) {
	case "and":
		evaluation.Children = make([]domainast.EvaluationNode, 0, len(node.Children))
		for index, child := range node.Children {
			childEval, err := evaluateNodeWithEvidence(ctx, child, runtime)
			if err != nil {
				return domainast.EvaluationNode{}, err
			}
			evaluation.Children = append(evaluation.Children, childEval)
			boolValue, ok := childEval.ReturnValue.(bool)
			if !ok {
				return domainast.EvaluationNode{}, fmt.Errorf("and expects boolean children")
			}
			if !boolValue {
				for _, skipped := range node.Children[index+1:] {
					evaluation.Children = append(evaluation.Children, skippedEvaluation(skipped))
				}
				evaluation.ReturnValue = false
				return evaluation, nil
			}
		}
		evaluation.ReturnValue = true
		return evaluation, nil
	case "or":
		evaluation.Children = make([]domainast.EvaluationNode, 0, len(node.Children))
		for index, child := range node.Children {
			childEval, err := evaluateNodeWithEvidence(ctx, child, runtime)
			if err != nil {
				return domainast.EvaluationNode{}, err
			}
			evaluation.Children = append(evaluation.Children, childEval)
			boolValue, ok := childEval.ReturnValue.(bool)
			if !ok {
				return domainast.EvaluationNode{}, fmt.Errorf("or expects boolean children")
			}
			if boolValue {
				for _, skipped := range node.Children[index+1:] {
					evaluation.Children = append(evaluation.Children, skippedEvaluation(skipped))
				}
				evaluation.ReturnValue = true
				return evaluation, nil
			}
		}
		evaluation.ReturnValue = false
		return evaluation, nil
	case "not":
		if len(node.Children) != 1 {
			return domainast.EvaluationNode{}, fmt.Errorf("not expects exactly one child")
		}
		childEval, err := evaluateNodeWithEvidence(ctx, node.Children[0], runtime)
		if err != nil {
			return domainast.EvaluationNode{}, err
		}
		boolValue, ok := childEval.ReturnValue.(bool)
		if !ok {
			return domainast.EvaluationNode{}, fmt.Errorf("not expects boolean child")
		}
		evaluation.Children = []domainast.EvaluationNode{childEval}
		evaluation.ReturnValue = !boolValue
		return evaluation, nil
	case "coalesce":
		evaluation.Children = make([]domainast.EvaluationNode, 0, len(node.Children))
		for index, child := range node.Children {
			childEval, err := evaluateNodeWithEvidence(ctx, child, runtime)
			if err != nil {
				return domainast.EvaluationNode{}, err
			}
			evaluation.Children = append(evaluation.Children, childEval)
			if childEval.ReturnValue != nil {
				for _, skipped := range node.Children[index+1:] {
					evaluation.Children = append(evaluation.Children, skippedEvaluation(skipped))
				}
				evaluation.ReturnValue = childEval.ReturnValue
				return evaluation, nil
			}
		}
		evaluation.ReturnValue = nil
		return evaluation, nil
	}

	if len(node.Children) > 0 {
		evaluation.Children = make([]domainast.EvaluationNode, len(node.Children))
		for i, child := range node.Children {
			childEval, err := evaluateNodeWithEvidence(ctx, child, runtime)
			if err != nil {
				return domainast.EvaluationNode{}, err
			}
			evaluation.Children[i] = childEval
		}
	}
	if len(node.NamedChildren) > 0 {
		evaluation.NamedChildren = make(map[string]domainast.EvaluationNode, len(node.NamedChildren))
		for name, child := range node.NamedChildren {
			childEval, err := evaluateNodeWithEvidence(ctx, child, runtime)
			if err != nil {
				return domainast.EvaluationNode{}, err
			}
			evaluation.NamedChildren[name] = childEval
		}
	}

	value, err := EvaluateNode(ctx, node, runtime)
	if err != nil {
		return domainast.EvaluationNode{}, err
	}
	evaluation.ReturnValue = value
	return evaluation, nil
}

func skippedEvaluation(node domainast.Node) domainast.EvaluationNode {
	evaluation := domainast.EvaluationNode{
		Function: node.Function,
		Constant: node.Constant,
		Skipped:  true,
	}
	if len(node.Children) > 0 {
		evaluation.Children = make([]domainast.EvaluationNode, len(node.Children))
		for i, child := range node.Children {
			evaluation.Children[i] = skippedEvaluation(child)
		}
	}
	if len(node.NamedChildren) > 0 {
		evaluation.NamedChildren = make(map[string]domainast.EvaluationNode, len(node.NamedChildren))
		for name, child := range node.NamedChildren {
			evaluation.NamedChildren[name] = skippedEvaluation(child)
		}
	}
	return evaluation
}
