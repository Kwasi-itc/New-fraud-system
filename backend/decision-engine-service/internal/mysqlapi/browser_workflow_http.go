package mysqlapi

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RegisterBrowser requires the application's trusted authentication middleware.
// Matching the source internal URL does not trust X-Workflow-* actor claims.
func (h *ExecutionHTTP) RegisterBrowser(group *gin.RouterGroup) {
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "private, no-store"); c.Next() })
	p := "/tenants/:tenantId/scenarios/:scenarioId"
	group.GET(p, h.handle("browser.list"))
	group.POST(p+"/copy", h.handle("browser.copy"))
	group.PUT(p+"/rules/:ruleId", h.handle("browser.save"))
	group.DELETE(p+"/rules/:ruleId", h.handle("browser.delete"))
}
func (h *ExecutionHTTP) browserWorkflow(c *gin.Context, a *mysql.BrowserWorkflowAdmin, op string) (any, error) {
	c.Header("Cache-Control", "private, no-store")
	if a == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	parent := uuid.MustParse(c.Param("scenarioId"))
	ctx := c.Request.Context()
	if op == "list" {
		return a.List(ctx, parent)
	}
	if op == "copy" {
		var in struct {
			DestinationID string `json:"destination_scenario_id"`
		}
		if err := decodeExecutionHTTP(c.Request.Body, min(h.maxBytes, 16384), &in); err != nil {
			return nil, err
		}
		destination, err := uuid.Parse(in.DestinationID)
		if err != nil || destination == uuid.Nil || destination == parent {
			return nil, errExecutionHTTPBody
		}
		values, err := a.Copy(ctx, parent, destination)
		if err != nil {
			return nil, err
		}
		return gin.H{"rules": values}, nil
	}
	if op == "delete" {
		var in workflow.CaseRule
		if e := decodeExecutionHTTP(c.Request.Body, min(h.maxBytes, 16384), &in); e != nil {
			return nil, e
		}
		if in.ID != c.Param("ruleId") || in.UpdatedAt == nil || in.UpdatedAt.IsZero() {
			return nil, errExecutionHTTPBody
		}
		return gin.H{"deleted": in.ID}, a.Delete(ctx, parent, uuid.MustParse(in.ID), *in.UpdatedAt)
	}
	var in workflow.CaseRule
	if e := decodeExecutionHTTP(c.Request.Body, min(h.maxBytes, 16384), &in); e != nil {
		return nil, e
	}
	if in.ID != c.Param("ruleId") {
		return nil, errExecutionHTTPBody
	}
	if e := in.Validate(); e != nil {
		return nil, errExecutionHTTPBody
	}
	return a.Save(ctx, parent, in)
}
