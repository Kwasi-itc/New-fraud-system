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
	logger       *slog.Logger
	db           *pgxpool.Pool
	dependencies []ReadinessDependency
}

type ReadinessPinger interface {
	Ping(context.Context) error
}

type ReadinessDependency struct {
	Name   string
	Pinger ReadinessPinger
}

func NewHealthHandler(logger *slog.Logger, db *pgxpool.Pool, dependencies ...ReadinessDependency) HealthHandler {
	return HealthHandler{
		logger:       logger,
		db:           db,
		dependencies: dependencies,
	}
}

func (h HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func (h HealthHandler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			h.logger.Error("readiness probe failed", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unready",
				"error":  "database_unreachable",
			})
			return
		}
	}
	for _, dependency := range h.dependencies {
		if dependency.Pinger == nil {
			continue
		}
		if err := dependency.Pinger.Ping(ctx); err != nil {
			h.logger.Error("readiness dependency probe failed", "dependency", dependency.Name, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":     "unready",
				"error":      "dependency_unreachable",
				"dependency": dependency.Name,
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
