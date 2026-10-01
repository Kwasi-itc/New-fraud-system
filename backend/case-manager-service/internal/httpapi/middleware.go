package httpapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AuthConfig struct {
	Mode      string
	Token     string
	TenantIDs []uuid.UUID
}

func requestContextMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request completed", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	}
}

func authMiddleware(cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Mode != "token" || cfg.Token == "" || len(cfg.TenantIDs) == 0 || subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+cfg.Token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Request = c.Request.WithContext(access.WithPrincipal(c.Request.Context(), access.Principal{Kind: access.Service, Subject: "case-integration", ServiceTenants: cfg.TenantIDs}))
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

func userMiddleware(verifier *access.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		p, err := verifier.Verify(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Request = c.Request.WithContext(access.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}
