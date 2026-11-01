package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"testing"
)

func mustScreeningRouter(t *testing.T, logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	t.Helper()
	router, err := NewRouter(logger, db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return router
}
