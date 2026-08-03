package handlers

import "github.com/gin-gonic/gin"

type ReadMetricsProvider interface {
	Snapshot() any
}

type ReadMetricsHandler struct {
	provider ReadMetricsProvider
}

func NewReadMetricsHandler(provider ReadMetricsProvider) ReadMetricsHandler {
	return ReadMetricsHandler{provider: provider}
}

func (h ReadMetricsHandler) Get(c *gin.Context) {
	c.JSON(200, gin.H{
		"read_metrics": h.provider.Snapshot(),
	})
}
