package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

var corsAllowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}
var corsAllowedHeaders = []string{"Authorization", "Content-Type", requestIDHeader}

func requestContextMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := c.GetHeader(requestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}

		requestLogger := logger.With("request_id", requestID)
		c.Set("logger", requestLogger)
		c.Set("request_id", requestID)
		c.Writer.Header().Set(requestIDHeader, requestID)
		c.Next()

		requestLogger.Info(
			"http request",
			"method", c.Request.Method,
			"path", c.FullPath(),
			"raw_path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		allowed[origin] = struct{}{}
	}

	allowedMethods := strings.Join(corsAllowedMethods, ", ")
	allowedHeaders := strings.Join(corsAllowedHeaders, ", ")

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Vary", "Origin")
				c.Writer.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				c.Writer.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
			}
		}

		if c.Request.Method == http.MethodOptions {
			if origin == "" {
				c.Status(http.StatusNoContent)
				return
			}
			if _, ok := allowed[origin]; ok {
				c.Status(http.StatusNoContent)
				return
			}
			c.Status(http.StatusForbidden)
			return
		}

		c.Next()
	}
}
