package handlers

import (
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h CaseHandler) Explore(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var in casepkg.ExplorationRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		presentError(c, casepkg.Invalid("invalid exploration request"))
		return
	}
	result, err := h.service.Explore(c.Request.Context(), tenant, id, in)
	if err != nil {
		presentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(200, result)
}

func (h CaseHandler) AttachExploredDecision(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var in casepkg.ExplorationAttach
	if err := c.ShouldBindJSON(&in); err != nil {
		presentError(c, casepkg.Invalid("invalid exploration attachment"))
		return
	}
	result, err := h.service.AttachExploredDecision(c.Request.Context(), tenant, id, in)
	if err != nil {
		presentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(200, result)
}
