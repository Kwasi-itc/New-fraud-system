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
