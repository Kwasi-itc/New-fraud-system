package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h CaseHandler) DecisionSelections(c *gin.Context) {
	tenant, ok := tenantID(c)
	if !ok {
		return
	}
	var body struct {
		DecisionIDs []uuid.UUID `json:"decision_ids"`
	}
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	items, err := h.service.DecisionSelections(c.Request.Context(), tenant, body.DecisionIDs)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"decisions": items})
}
