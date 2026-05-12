package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/reconcile"
)

type ReconcileHandler struct {
	service reconcile.Service
}

func NewReconcileHandler(service reconcile.Service) ReconcileHandler {
	return ReconcileHandler{service: service}
}

func (h ReconcileHandler) Run(c *gin.Context) {
	report, err := h.service.Run(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "reconcile_failed",
				"message": err.Error(),
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"report": report})
}
