package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func requestContextMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("logger", logger)
		c.Next()
	}
}
