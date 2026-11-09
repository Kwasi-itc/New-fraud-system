package mysqlapi

import (
	"bytes"
	"io"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerAuthoring(v1 *gin.RouterGroup) {
	s := "/tenants/:tenantId/scenarios"
	i := s + "/:scenarioId/iterations"
	r := i + "/:iterationId/rules"
	for _, route := range []struct{ method, path, op string }{
		{"GET", s, "scenarios"}, {"POST", s, "scenario.create"},
		{"GET", s + "/:scenarioId", "scenario.get"}, {"PUT", s + "/:scenarioId", "scenario.update"}, {"DELETE", s + "/:scenarioId", "scenario.delete"},
		{"POST", s + "/:scenarioId/copy", "scenario.copy"},
		{"GET", i, "iterations"}, {"POST", i, "iteration.create"}, {"GET", i + "/metadata", "iterations.metadata"},
		{"GET", i + "/:iterationId", "iteration.get"}, {"PUT", i + "/:iterationId", "iteration.update"},
		{"POST", i + "/:iterationId/draft", "iteration.clone"}, {"POST", i + "/:iterationId/commit", "iteration.commit"},
		{"GET", r, "rules"}, {"POST", r, "rule.create"}, {"PUT", r + "/:ruleId", "rule.update"}, {"DELETE", r + "/:ruleId", "rule.delete"},
		{"GET", s + "/:scenarioId/rule-groups", "rule.groups"},
		{"GET", s + "/:scenarioId/rules/latest", "rules.latest"},
		{"POST", i + "/:iterationId/validate", "iteration.validate"},
	} {
		v1.Handle(route.method, route.path, h.handle("authoring."+route.op))
	}
}

// Commands with no parameters accept an absent body or an empty object only.
func (h *ExecutionHTTP) emptyAuthoringBody(c *gin.Context) error {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, h.maxBytes+1))
	if err != nil {
		return errExecutionHTTPBody
	}
	if int64(len(raw)) > h.maxBytes {
		return errExecutionHTTPSize
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return decodeExecutionHTTP(bytes.NewReader(raw), h.maxBytes, &struct{}{})
}

func (h *ExecutionHTTP) authoring(c *gin.Context, a *mysqlexecution.Authoring, op string) (out any, status int, err error) {
	defer func() {
		// Only a direct statement error has a confirmed rollback here. Keep
		// CommitError/RollbackError wrappers intact for unknown-outcome replies.
		if conflict, ok := err.(*mysqldriver.MySQLError); ok && (conflict.Number == 1062 || conflict.Number == 1451) {
			err = mysql.ErrOwnerRelationshipConflict
		}
	}()
	if a == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	ctx := c.Request.Context()
	param := func(name string) uuid.UUID {
		if c.Param(name) == "" {
			return uuid.Nil
		}
		return uuid.MustParse(c.Param(name))
	}
	parent, iteration, rule := param("scenarioId"), param("iterationId"), param("ruleId")
	status = http.StatusOK
	switch op {
	case "scenario.copy":
		var in dto.CopyScenarioRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &in); err != nil {
			return nil, 0, err
		}
		value, err := a.CopyScenario(ctx, parent, in.Name)
		if err != nil {
			return nil, 0, err
		}
		return gin.H{"scenario": dto.AdaptScenario(value)}, http.StatusCreated, nil
	case "iteration.validate":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		result, err := a.Validate(ctx, parent, iteration)
		if err != nil {
			return nil, 0, err
		}
		if !result.Valid {
			status = http.StatusUnprocessableEntity
		}
		return gin.H{"validation": dto.AdaptIterationValidation(result)}, status, nil
	case "rules.latest":
		values, err := a.LatestRules(ctx, parent)
		if err != nil {
			return nil, 0, err
		}
		out := make([]dto.RuleResponse, len(values))
		for i, v := range values {
			out[i] = dto.AdaptRule(v)
		}
		return gin.H{"rules": out}, status, nil
	case "scenarios":
		values, err := a.Scenarios(ctx)
		if err != nil {
			return nil, 0, err
		}
		out := make([]dto.ScenarioResponse, len(values))
		for i, v := range values {
			out[i] = dto.AdaptScenario(v)
		}
		return gin.H{"scenarios": out}, status, nil
	case "scenario.get":
		v, err := a.Scenario(ctx, parent)
		return gin.H{"scenario": dto.AdaptScenario(v)}, status, err
	case "scenario.create", "scenario.update":
		var req dto.CreateScenarioRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		if op == "scenario.create" {
			status = http.StatusCreated
		}
		v, err := a.SaveScenario(ctx, parent, req.Name, req.Description, req.TriggerObjectType)
		return gin.H{"scenario": dto.AdaptScenario(v)}, status, err
	case "scenario.delete":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		return nil, http.StatusNoContent, a.DeleteScenario(ctx, parent)
	case "iterations", "iterations.metadata":
		values, err := a.Iterations(ctx, parent)
		if err != nil {
			return nil, 0, err
		}
		if op == "iterations.metadata" {
			out := make([]dto.MetadataIterationResponse, len(values))
			for i, v := range values {
				out[i] = dto.AdaptIterationMetadata(v)
			}
			return gin.H{"iterations": out}, status, nil
		}
		out := make([]dto.IterationResponse, len(values))
		for i, v := range values {
			out[i] = dto.AdaptIteration(v)
		}
		return gin.H{"iterations": out}, status, nil
	case "iteration.get":
		v, err := a.Iteration(ctx, parent, iteration)
		return gin.H{"iteration": dto.AdaptIteration(v)}, status, err
	case "iteration.create", "iteration.clone", "iteration.commit":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		var v scenario.Iteration
		var err error
		switch op {
		case "iteration.create":
			v, err = a.CreateDraft(ctx, parent)
			status = http.StatusCreated
		case "iteration.clone":
			v, err = a.CloneDraft(ctx, parent, iteration)
			status = http.StatusCreated
		case "iteration.commit":
			v, err = a.Commit(ctx, parent, iteration)
		}
		return gin.H{"iteration": dto.AdaptIteration(v)}, status, err
	case "iteration.update":
		var req dto.UpdateIterationRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		v, err := a.UpdateDraft(ctx, parent, iteration, scenario.Iteration{TriggerFormula: req.TriggerFormula, Schedule: req.Schedule, ScoreReviewThreshold: req.ScoreReviewThreshold, ScoreBlockAndReviewThreshold: req.ScoreBlockAndReviewThreshold, ScoreDeclineThreshold: req.ScoreDeclineThreshold})
		return gin.H{"iteration": dto.AdaptIteration(v)}, status, err
	case "rules":
		values, err := a.Rules(ctx, parent, iteration)
		if err != nil {
			return nil, 0, err
		}
		out := make([]dto.RuleResponse, len(values))
		for i, v := range values {
			out[i] = dto.AdaptRule(v)
		}
		return gin.H{"rules": out}, status, nil
	case "rule.groups":
		values, err := a.RuleGroups(ctx, parent)
		return gin.H{"rule_groups": values}, status, err
	case "rule.create", "rule.update":
		var req dto.CreateRuleRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		if op == "rule.create" {
			status = http.StatusCreated
		}
		v, err := a.SaveRule(ctx, parent, iteration, rule, scenario.Rule{DisplayOrder: req.DisplayOrder, Name: req.Name, Description: req.Description, Formula: req.Formula, ScoreModifier: req.ScoreModifier, RuleGroup: req.RuleGroup, SnoozeGroupID: req.SnoozeGroupID, StableRuleID: req.StableRuleID})
		return gin.H{"rule": dto.AdaptRule(v)}, status, err
	case "rule.delete":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		return nil, http.StatusNoContent, a.DeleteRule(ctx, parent, iteration, rule)
	default:
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
}
