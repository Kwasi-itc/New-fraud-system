package httpapi

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func requestContextMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := uuid.NewString()
		c.Header("X-Request-ID", requestID)
		log := logger.With("request_id", requestID, "method", c.Request.Method)
		c.Request = c.Request.WithContext(observability.WithLogger(c.Request.Context(), log))
		log.Debug("request started", "event", "http.request.started", "route", c.FullPath())
		c.Next()
		fields := []any{"event", "http.request.completed", "route", c.FullPath(), "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds(), "response_bytes", c.Writer.Size()}
		for _, name := range []string{"tenantId", "caseId", "inboxId", "resourceId", "reportId", "fileId", "uploadId"} {
			if id, err := uuid.Parse(c.Param(name)); err == nil {
				fields = append(fields, strings.TrimSuffix(name, "Id")+"_id", id.String())
			}
		}
		if p, ok := access.FromContext(c.Request.Context()); ok {
			fields = append(fields, "actor_kind", p.Kind, "actor_id", p.Subject)
		}
		if code, ok := c.Get("log_error_code"); ok {
			fields = append(fields, "error_code", code)
		}
		level := slog.LevelInfo
		if c.FullPath() == "/healthz" || c.FullPath() == "/readyz" {
			level = slog.LevelDebug
		}
		if c.Writer.Status() >= 400 {
			level = slog.LevelWarn
		}
		if c.Writer.Status() >= 500 {
			level = slog.LevelError
		}
		log.Log(c.Request.Context(), level, "request completed", fields...)
	}
}

func recoverRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				observability.Logger(c.Request.Context()).Error("request panic recovered", "event", "http.request.panic", "route", c.FullPath(), "stack", string(debug.Stack()))
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}

func requestDeadline(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
