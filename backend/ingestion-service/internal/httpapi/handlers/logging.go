package handlers

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func loggerFromContext(c *gin.Context) *slog.Logger {
	value, ok := c.Get("logger")
	if !ok {
		return slog.Default()
	}
	logger, ok := value.(*slog.Logger)
	if !ok || logger == nil {
		return slog.Default()
	}
	return logger
}

func requestIDFromContext(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}

func logHandlerSuccess(c *gin.Context, operation string, attrs ...any) {
	loggerFromContext(c).Info(operation, attrs...)
}

func logHandlerFailure(c *gin.Context, operation string, err error, attrs ...any) {
	if err != nil {
		attrs = append(attrs, "error", err)
		loggerFromContext(c).Error(operation, attrs...)
		return
	}
	loggerFromContext(c).Warn(operation, attrs...)
}
