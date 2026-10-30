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

// Applied only to the legacy owner route group. Manual search and investigator
// routes are registered separately and retain live identity/resource checks.
func screeningOwnerBoundary(token, serviceToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if len(strings.TrimSpace(token)) < 32 || token == serviceToken ||
			subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Screening-Operator-Token")), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "screening_owner_requires_private_operator"})
			return
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
