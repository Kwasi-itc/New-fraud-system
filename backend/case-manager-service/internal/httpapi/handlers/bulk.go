package handlers

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h CaseHandler) Bulk(c *gin.Context) {
	tenant, ok := tenantID(c)
	if !ok {
		return
	}
	var input service.BulkInput
	if err := bindJSON(c, &input); err != nil {
		presentError(c, err)
		return
	}
	results, err := h.service.Bulk(c.Request.Context(), tenant, input)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation_id": input.OperationID, "results": results})
}
