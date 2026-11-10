package mysqlapi

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) RegisterBrowserScreening(group *gin.RouterGroup) {
	p := "/tenants/:tenantId"
	group.GET(p+"/scenarios/:scenarioId", h.handle("browser-screening.list"))
	group.PUT(p+"/scenarios/:scenarioId/configs/:configId", h.handle("browser-screening.save"))
	group.GET(p+"/decisions/:decisionId/screenings", h.handle("browser-screening.summary"))
}

func (h *ExecutionHTTP) browserScreening(c *gin.Context, a *mysql.BrowserWorkflowAdmin, op string) {
	c.Header("Cache-Control", "private, no-store")
	if a == nil {
		executionHTTPError(c, mysql.ErrDecisionAuthorizationDenied)
		return
	}
	var out any
	var err error
	switch op {
	case "browser-screening.list":
		out, err = a.ScreeningPage(c.Request.Context(), uuid.MustParse(c.Param("scenarioId")))
	case "browser-screening.save":
		var in screening.AuthoringRule
		if err = decodeExecutionHTTP(c.Request.Body, min(h.maxBytes, 8192), &in); err == nil {
			if in.ID != c.Param("configId") {
				err = errExecutionHTTPBody
			} else {
				out, err = a.SaveScreening(c.Request.Context(), uuid.MustParse(c.Param("scenarioId")), in)
			}
		}
	case "browser-screening.summary":
		values, e := a.ScreeningSummaries(c.Request.Context(), uuid.MustParse(c.Param("decisionId")))
		out, err = gin.H{"screening_executions": values}, e
	}
	if err != nil {
		if errors.Is(err, screening.ErrAuthoringConflict) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, screening.ErrMatchingUnavailable) {
			c.JSON(422, gin.H{"error": "These provider settings are unavailable in local development."})
			return
		}
		executionHTTPError(c, err)
		return
	}
	c.JSON(200, out)
}
