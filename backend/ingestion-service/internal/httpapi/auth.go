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

func authMiddleware(cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Mode == "disabled" {
			c.Next()
			return
		}
		if cfg.Mode != "token" || strings.TrimSpace(cfg.Token) == "" {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"code":    "auth_misconfigured",
					"message": "invalid service authentication configuration",
				},
			})
			return
		}

		authHeader := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "unauthorized",
					"message": "missing bearer token",
				},
			})
			return
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "unauthorized",
					"message": "invalid bearer token",
				},
			})
			return
		}
		c.Next()
	}
}
