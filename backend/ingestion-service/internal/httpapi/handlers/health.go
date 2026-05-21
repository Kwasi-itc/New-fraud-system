package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewHealthHandler(logger *slog.Logger, db *pgxpool.Pool) HealthHandler {
	return HealthHandler{
		logger: logger,
		db:     db,
	}
}

func (h HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func (h HealthHandler) Readyz(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{
			"status": "ready",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.logger.Error("readiness probe failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unready",
			"error":  "database_unreachable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
