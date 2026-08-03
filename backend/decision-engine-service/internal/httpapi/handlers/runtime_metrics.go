package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type RuntimeMetricsHandler struct {
	decisionService service.DecisionService
}

func NewRuntimeMetricsHandler(decisionService service.DecisionService) RuntimeMetricsHandler {
	return RuntimeMetricsHandler{decisionService: decisionService}
}

func (h RuntimeMetricsHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"runtime_metrics": h.decisionService.RuntimeMetrics(),
	})
}
