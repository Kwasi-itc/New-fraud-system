package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-Id"

func requestContextMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(requestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Header(requestIDHeader, requestID)
		c.Set("request_id", requestID)
		c.Set("logger", logger.With("request_id", requestID))
		c.Next()
	}
}

func requestLoggingMiddleware(logger *slog.Logger, metrics *serviceMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		duration := time.Since(startedAt)
		metrics.Record(c.Request.Method, route, c.Writer.Status(), duration)

		logger.Info("http request completed",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"route", route,
			"status_code", c.Writer.Status(),
			"duration_ms", duration.Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}
