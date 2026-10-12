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
