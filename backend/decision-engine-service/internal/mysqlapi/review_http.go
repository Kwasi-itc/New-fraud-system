package mysqlapi

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerReview(v *gin.RouterGroup) {
	path := "/investigator/tenants/:tenantId/cases/:caseId/review"
	v.GET(path, h.handle("review.get"))
	v.POST(path, h.handle("review.post"))
}
func (h *ExecutionHTTP) review(c *gin.Context, api *ExecutionEndpointScope, op string) {
	c.Header("Cache-Control", "private, no-store")
	if api.Review == nil {
		executionHTTPError(c, mysql.ErrOwnerContractUnavailable)
		return
	}
	if !api.Read.store.MatchesOwner(api.Review) {
		executionHTTPError(c, mysql.ErrTenantScope)
		return
	}
	if len(c.Request.URL.RawQuery) > 4096 {
		executionHTTPError(c, errExecutionHTTPSize)
		return
	}
	var command *identity.Command
	resource := ""
	if op == "review.get" {
		q := c.Request.URL.Query()
		if len(q) != 1 || len(q["resource_id"]) != 1 {
			executionHTTPError(c, errExecutionHTTPBody)
			return
		}
		resource = q.Get("resource_id")
	} else {
		if c.Request.URL.RawQuery != "" {
			executionHTTPError(c, errExecutionHTTPBody)
			return
		}
		command = &identity.Command{}
		if err := decodeExecutionHTTP(c.Request.Body, min(h.maxBytes, 16384), command); err != nil {
			executionHTTPError(c, err)
			return
		}
		resource = command.ResourceID
	}
	id, err := uuid.Parse(resource)
	if err != nil || id == uuid.Nil {
		executionHTTPError(c, errExecutionHTTPBody)
		return
	}
	out, err := api.Review.Review(c.Request.Context(), uuid.MustParse(c.Param("caseId")), id, command)
	if err != nil {
		status, message := identity.HTTPError(err)
		if status == 500 {
			executionHTTPError(c, err)
		} else {
			c.AbortWithStatusJSON(status, gin.H{"error": message})
		}
		return
	}
	c.JSON(200, out)
}
