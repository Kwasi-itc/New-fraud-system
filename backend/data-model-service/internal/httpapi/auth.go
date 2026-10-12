package httpapi

import (
	"crypto/subtle"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type AuthConfig struct {
	Mode     string
	Token    string
	Verifier platformVerifier
	DB       access.Queryer
}

func authMiddleware(cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Verifier != nil {
			if cfg.Mode != "token" || strings.TrimSpace(cfg.Token) == "" {
				platformError(c, 500, "Invalid service authentication configuration.")
				return
			}
			// A configured service credential remains a distinct machine boundary.
			// Browser proxies forward only platform JWTs, never this credential.
			if subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+cfg.Token)) != 1 {
				authorizePlatform(c, cfg.Verifier, cfg.DB)
				return
			}
		}
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
