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
	return HealthHandler{logger: logger, db: db}
}

func (h HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "screening-service",
	})
}

func (h HealthHandler) Readyz(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database_unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.logger.Error("database readiness ping failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database_unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
