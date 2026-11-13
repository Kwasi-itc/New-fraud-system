package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type AuthConfig struct {
	Mode  string
	Token string
}

// Raw execution payloads and recovery mutations are operator contracts, not
// analyst APIs. The session-derived read gateway returns a safe summary instead.
func screeningExecutionBoundary(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.FullPath() {
		case "/v1/tenants/:tenantId/decisions/:decisionId/screening-executions",
			"/v1/tenants/:tenantId/screening-executions/:executionId",
			"/v1/tenants/:tenantId/screening-executions/:executionId/status",
			"/v1/tenants/:tenantId/screening-executions/:executionId/retry":
			c.Header("Cache-Control", "private, no-store")
			if len(strings.TrimSpace(token)) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Screening-Authoring-Token")), []byte(token)) != 1 {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "screening_execution_requires_private_operator"})
				return
			}
		}
		c.Next()
	}
}

// Legacy configuration routes are operator-only until an identity-checked
// authoring workflow is available. A browser-visible service token is not
// evidence of tenant membership or permission to alter live screening.
func screeningConfigurationBoundary(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.Contains(c.FullPath(), "/scenarios/:scenarioId/screening-configs") {
			if len(strings.TrimSpace(token)) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Screening-Authoring-Token")), []byte(token)) != 1 {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "screening_configuration_requires_private_gateway"})
				return
			}
		}
		c.Next()
	}
}

// Legacy workflow configuration APIs are operator-only. The browser editor uses
// the private authoring route with current identity and inbox checks instead.
func workflowConfigurationBoundary(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if strings.Contains(path, "/scenarios/:scenarioId/workflows") || strings.Contains(path, "/scenarios/:scenarioId/workflow-rules") {
			if len(token) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Workflow-Authoring-Token")), []byte(token)) != 1 {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "workflow_configuration_requires_private_gateway"})
				return
			}
		}
		c.Next()
	}
}

func authMiddleware(cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch cfg.Mode {
		case "disabled":
			c.Next()
			return
		case "token":
			if strings.TrimSpace(cfg.Token) == "" {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid_auth_configuration"})
				return
			}
			if subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+cfg.Token)) != 1 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "unauthorized",
				})
				return
			}
			c.Next()
			return
		default:
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "invalid_auth_mode",
			})
		}
	}
}

// Internal integration routes never inherit legacy disabled authentication.
func screeningServiceBoundary(token string, forbidden ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		valid := len(strings.TrimSpace(token)) >= 32
		for _, value := range forbidden {
			if token == value {
				valid = false
			}
		}
		if !valid {
			c.AbortWithStatusJSON(503, gin.H{"error": "screening_integration_unavailable"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
