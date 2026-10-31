package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewHealthHandler(logger *slog.Logger, db *pgxpool.Pool) HealthHandler {
	if logger == nil {
		logger = slog.Default()
	}
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
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unready",
			"error":  "database_unavailable",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := storepostgres.ValidateQueueReadiness(ctx, h.db, false); err != nil {
		h.logger.Error("readiness probe failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unready",
			"error":  "queue_or_database_unavailable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
