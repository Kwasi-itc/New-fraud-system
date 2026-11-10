package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
)

// NewMySQLRouter reuses the source domain and HTTP contract with native stores.
func NewMySQLRouter(logger *slog.Logger, db *sql.DB, database string, cfg RouterConfig) (*gin.Engine, error) {
	if logger == nil || db == nil || cfg.UserVerifier == nil {
		return nil, errors.New("case logger, database and user verifier required")
	}
	repos, e := store.NewRepositories(db, database)
	if e != nil {
		return nil, e
	}
	svc := service.NewCaseService(uuidGenerator{}, systemClock{}, repos, store.UnitOfWork{DB: db, Database: database})
	auth := func(g *gin.Context) {
		header := g.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			g.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		p, e := cfg.UserVerifier.Verify(strings.TrimPrefix(header, "Bearer "))
		if e != nil {
			g.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		e = store.CheckIdentity(g.Request.Context(), db, database, p, false)
		if e != nil {
			if errors.Is(e, c.ErrForbidden) {
				g.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			} else {
				g.AbortWithStatusJSON(503, gin.H{"error": "identity unavailable"})
			}
			return
		}
		g.Request = g.Request.WithContext(access.WithPrincipal(g.Request.Context(), p))
		g.Next()
	}
	health := func(g *gin.Context) { g.JSON(200, gin.H{"status": "ok"}) }
	ready := func(g *gin.Context) {
		ctx, cancel := context.WithTimeout(g.Request.Context(), 2*time.Second)
		defer cancel()
		if e := db.PingContext(ctx); e != nil {
			g.JSON(503, gin.H{"status": "not_ready"})
			return
		}
		g.JSON(200, gin.H{"status": "ready"})
	}
	return newRouter(logger, cfg, svc, auth, health, ready), nil
}
