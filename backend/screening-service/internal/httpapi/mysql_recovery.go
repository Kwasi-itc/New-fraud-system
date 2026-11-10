package httpapi

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"time"
)

func mysqlRecovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				log.Error("screening request failed", "request_id", c.GetString("request_id"), "method", c.Request.Method, "route", c.FullPath())
				c.AbortWithStatusJSON(500, gin.H{"error": "screening request could not be completed"})
			}
		}()
		c.Next()
	}
}
func mysqlRequestLog(log *slog.Logger, metrics *serviceMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		duration := time.Since(start)
		metrics.Record(c.Request.Method, route, c.Writer.Status(), duration)
		log.Info("http request completed", "request_id", c.GetString("request_id"), "method", c.Request.Method, "route", route, "status_code", c.Writer.Status(), "duration_ms", duration.Milliseconds())
	}
}
