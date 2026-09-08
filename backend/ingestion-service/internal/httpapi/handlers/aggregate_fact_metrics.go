package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type AggregateFactMetricsProvider interface {
	Snapshot() any
}

type AggregateFactMetricsHandler struct {
	provider AggregateFactMetricsProvider
}

func NewAggregateFactMetricsHandler(provider AggregateFactMetricsProvider) AggregateFactMetricsHandler {
	return AggregateFactMetricsHandler{provider: provider}
}

func (h AggregateFactMetricsHandler) Get(c *gin.Context) {
	metrics := any(map[string]any{})
	if h.provider != nil {
		metrics = h.provider.Snapshot()
	}
	c.JSON(http.StatusOK, gin.H{"aggregate_fact_metrics": metrics})
}
