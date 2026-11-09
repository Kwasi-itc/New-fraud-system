package mysqlapi

import (
	"bytes"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"strings"
)

func (h *ExecutionHTTP) registerWorkflowRules(v *gin.RouterGroup) {
	p := "/tenants/:tenantId/scenarios/:scenarioId/workflow-rules"
	for _, r := range []struct{ method, path, op string }{
		{"GET", p, "list"}, {"POST", p, "create"}, {"POST", p + "/reorder", "reorder"},
		{"GET", p + "/:ruleId", "get"}, {"PUT", p + "/:ruleId", "update"}, {"DELETE", p + "/:ruleId", "delete"},
		{"POST", p + "/:ruleId/conditions", "condition.create"}, {"PUT", p + "/:ruleId/conditions/:conditionId", "condition.update"}, {"DELETE", p + "/:ruleId/conditions/:conditionId", "condition.delete"},
		{"POST", p + "/:ruleId/actions", "action.create"}, {"PUT", p + "/:ruleId/actions/:actionId", "action.update"}, {"DELETE", p + "/:ruleId/actions/:actionId", "action.delete"},
	} {
		v.Handle(r.method, r.path, h.handle("workflow-rules."+r.op))
	}
}
func (h *ExecutionHTTP) workflowRules(c *gin.Context, a *ExecutionEndpointScope, op string) (any, int, error) {
	if a.Administration == nil || a.Authoring == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	s := a.Administration
	ctx := c.Request.Context()
	parent := uuid.MustParse(c.Param("scenarioId"))
	param := func(name string) uuid.UUID {
		if c.Param(name) == "" {
			return uuid.Nil
		}
		return uuid.MustParse(c.Param(name))
	}
	rule := param("ruleId")
	switch op {
	case "list":
		values, err := s.WorkflowRules(ctx, parent)
		return gin.H{"workflow_rules": adaptHistory(values, dto.AdaptStructuredWorkflow)}, 200, err
	case "get":
		item, err := s.WorkflowRule(ctx, parent, rule)
		return gin.H{"workflow_rule": dto.AdaptStructuredWorkflow(item)}, 200, err
	case "create", "update":
		var req dto.CreateWorkflowRuleRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		item, err := s.SaveWorkflowRule(ctx, parent, rule, req.Name, req.Fallthrough)
		status := 200
		if op == "create" {
			status = 201
		}
		return gin.H{"workflow_rule": dto.AdaptStructuredWorkflow(item)}, status, err
	case "delete":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		return nil, 204, s.DeleteWorkflowRule(ctx, parent, rule)
	case "reorder":
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, h.maxBytes+1))
		if err != nil {
			return nil, 0, errExecutionHTTPBody
		}
		if int64(len(raw)) > h.maxBytes {
			return nil, 0, errExecutionHTTPSize
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] != '[' {
			return nil, 0, errExecutionHTTPBody
		}
		var wrapped struct {
			IDs []*string `json:"rule_ids"`
		}
		encoded := append([]byte(`{"rule_ids":`), raw...)
		encoded = append(encoded, '}')
		if err := decodeExecutionHTTP(bytes.NewReader(encoded), h.maxBytes+16, &wrapped); err != nil {
			return nil, 0, err
		}
		if wrapped.IDs == nil {
			return nil, 0, errExecutionHTTPBody
		}
		ids := make([]string, len(wrapped.IDs))
		for i, id := range wrapped.IDs {
			if id == nil {
				return nil, 0, errExecutionHTTPBody
			}
			ids[i] = *id
		}
		return nil, 204, s.ReorderWorkflowRules(ctx, parent, ids)
	}
	if strings.HasPrefix(op, "condition.") {
		id := param("conditionId")
		if op == "condition.delete" {
			if err := h.emptyAuthoringBody(c); err != nil {
				return nil, 0, err
			}
			return nil, 204, s.DeleteWorkflowChild(ctx, parent, rule, id, false)
		}
		var req dto.WorkflowConditionRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		fields := workflow.Condition{TenantID: c.Param("tenantId"), RuleID: rule.String(), Function: workflow.ConditionType(req.Function), Params: req.Params}
		if err := a.Authoring.ValidateWorkflowCondition(ctx, parent, fields); err != nil {
			return nil, 0, errors.Join(mysql.ErrAdministrationInput, err)
		}
		item, err := s.SaveWorkflowCondition(ctx, parent, rule, id, fields)
		status := 200
		if op == "condition.create" {
			status = 201
		}
		return gin.H{"condition": dto.AdaptWorkflowConditionRecord(item)}, status, err
	}
	if strings.HasPrefix(op, "action.") {
		id := param("actionId")
		if op == "action.delete" {
			if err := h.emptyAuthoringBody(c); err != nil {
				return nil, 0, err
			}
			return nil, 204, s.DeleteWorkflowChild(ctx, parent, rule, id, true)
		}
		var req dto.WorkflowActionRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		fields := workflow.Action{TenantID: c.Param("tenantId"), RuleID: rule.String(), ActionType: workflow.ActionType(req.ActionType), ActionConfig: req.ActionConfig}
		if err := fields.Validate(); err != nil {
			return nil, 0, errors.Join(mysql.ErrAdministrationInput, err)
		}
		item, err := s.SaveWorkflowAction(ctx, parent, rule, id, fields)
		status := 200
		if op == "action.create" {
			status = 201
		}
		return gin.H{"action": dto.AdaptWorkflowActionRecord(item)}, status, err
	}
	return nil, 0, errExecutionHTTPBody
}
