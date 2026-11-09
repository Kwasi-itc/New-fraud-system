package mysqlapi

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerAdministration(v *gin.RouterGroup) {
	path := "/tenants/:tenantId/scenarios/:scenarioId/workflows"
	for _, r := range []struct{ method, path, op string }{{"GET", path, "workflows"}, {"POST", path, "workflow.create"}, {"GET", path + "/:workflowId", "workflow.get"}, {"PUT", path + "/:workflowId", "workflow.update"}, {"DELETE", path + "/:workflowId", "workflow.delete"}, {"POST", path + "/reorder", "workflow.reorder"}, {"POST", "/tenants/:tenantId/scenarios/:scenarioId/test-runs", "run.create"}, {"POST", "/tenants/:tenantId/test-runs/:testRunId/cancel", "run.cancel"}, {"POST", "/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes", "snooze.create"}} {
		v.Handle(r.method, r.path, h.handle("admin."+r.op))
	}
}

type workflowRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	AllowedOutcomes []*string       `json:"allowed_outcomes"`
	ActionType      string          `json:"action_type"`
	ActionConfig    json.RawMessage `json:"action_config"`
	Active          bool            `json:"active"`
}

func (h *ExecutionHTTP) administration(c *gin.Context, s *mysql.AdministrationStore, op string) (out any, status int, err error) {
	defer func() {
		if e, ok := err.(*mysqldriver.MySQLError); ok && (e.Number == 1062 || e.Number == 1451 || e.Number == 1452) {
			err = mysql.ErrOwnerRelationshipConflict
		}
	}()
	if s == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	ctx := c.Request.Context()
	param := func(name string) uuid.UUID {
		if c.Param(name) == "" {
			return uuid.Nil
		}
		return uuid.MustParse(c.Param(name))
	}
	p, id := param("scenarioId"), param("workflowId")
	switch op {
	case "workflows":
		v, e := s.Workflows(ctx, p)
		return gin.H{"workflows": adaptHistory(v, dto.AdaptWorkflow)}, 200, e
	case "workflow.get":
		v, e := s.Workflow(ctx, p, id)
		return gin.H{"workflow": dto.AdaptWorkflow(v)}, 200, e
	case "workflow.create", "workflow.update":
		var req workflowRequest
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		outcomes := make([]string, len(req.AllowedOutcomes))
		for i, v := range req.AllowedOutcomes {
			if v == nil {
				return nil, 0, errExecutionHTTPBody
			}
			outcomes[i] = *v
		}
		v, e := s.SaveWorkflow(ctx, p, id, workflow.Definition{Name: req.Name, Description: req.Description, AllowedOutcomes: outcomes, ActionType: workflow.ActionType(req.ActionType), ActionConfig: req.ActionConfig, Active: req.Active})
		status = 200
		if op == "workflow.create" {
			status = 201
		}
		return gin.H{"workflow": dto.AdaptWorkflow(v)}, status, e
	case "workflow.delete":
		if e := h.emptyAuthoringBody(c); e != nil {
			return nil, 0, e
		}
		return nil, 204, s.DeleteWorkflow(ctx, p, id)
	case "workflow.reorder":
		raw, e := io.ReadAll(io.LimitReader(c.Request.Body, h.maxBytes+1))
		if e != nil {
			return nil, 0, errExecutionHTTPBody
		}
		if int64(len(raw)) > h.maxBytes {
			return nil, 0, errExecutionHTTPSize
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] != '[' {
			return nil, 0, errExecutionHTTPBody
		}
		var req struct {
			IDs []*string `json:"workflow_ids"`
		}
		wrapped := append([]byte(`{"workflow_ids":`), raw...)
		wrapped = append(wrapped, '}')
		if e := decodeExecutionHTTP(bytes.NewReader(wrapped), h.maxBytes+32, &req); e != nil {
			return nil, 0, e
		}
		ids := make([]string, len(req.IDs))
		for i, id := range req.IDs {
			if id == nil {
				return nil, 0, errExecutionHTTPBody
			}
			ids[i] = *id
		}
		return nil, 204, s.ReorderWorkflows(ctx, p, ids)
	case "run.create":
		var req dto.CreateTestRunRequest
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		phantom, e := uuid.Parse(req.PhantomIterationID)
		if e != nil || phantom == uuid.Nil {
			return nil, 0, errExecutionHTTPBody
		}
		v, e := s.CreateTestRun(ctx, p, phantom, req.ExpiresAt)
		return gin.H{"test_run": dto.AdaptTestRun(v)}, 201, e
	case "run.cancel":
		if e := h.emptyAuthoringBody(c); e != nil {
			return nil, 0, e
		}
		v, e := s.CancelTestRun(ctx, param("testRunId"))
		return gin.H{"test_run": dto.AdaptTestRun(v)}, 200, e
	case "snooze.create":
		var req dto.CreateRuleSnoozeRequest
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		v, e := s.CreateSnooze(ctx, p, snooze.RuleSnooze{ObjectType: req.ObjectType, ObjectID: req.ObjectID, SnoozeGroupID: req.SnoozeGroupID, ExpiresAt: req.ExpiresAt})
		return gin.H{"rule_snooze": dto.AdaptRuleSnooze(v)}, 201, e
	default:
		return nil, 0, mysql.ErrAdministrationInput
	}
}
