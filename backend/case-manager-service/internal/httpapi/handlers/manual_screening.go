package handlers

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h CaseHandler) InvestigateManualScreening(c *gin.Context) {
	tenant, ok := tenantID(c)
	if !ok {
		return
	}
	var in service.ManualScreeningInput
	if err := bindJSON(c, &in); err != nil {
		presentError(c, err)
		return
	}
	in.TenantID = tenant
	item, err := h.service.InvestigateManualScreening(c.Request.Context(), in)
	if err != nil {
		presentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, gin.H{"case": item})
}
