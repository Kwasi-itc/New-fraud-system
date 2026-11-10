package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type MySQLDependencies struct {
	Provider     ports.ScreeningProvider
	Capabilities provider.ManualCapabilities
	Inbox        ports.InboxReader
	Blob         ports.BlobStore
	Cases        ports.CasePublisher
	Decisions    ports.DecisionEnginePublisher
}

func MySQLService(s *store.Store, deps MySQLDependencies) service.ScreeningService {
	polling := store.DomainPollingEnqueuer{}
	return service.NewScreeningService(s, uuidGenerator{}, systemClock{}, s.Screenings(), s.ScreeningMatches(), s.ScreeningComments(), s.Whitelist(), s.ScreeningFiles(), s.ContinuousConfigs(), s.MonitoredObjects(), s.DatasetUpdateJobs(), deps.Provider, deps.Inbox, deps.Cases, deps.Blob, deps.Decisions, polling, polling, polling)
}
func NewMySQLRouter(log *slog.Logger, db *sql.DB, database string, cfg RouterConfig, deps MySQLDependencies) (*gin.Engine, error) {
	if cfg.InvestigatorVerifier == nil || deps.Provider == nil || deps.Inbox == nil || deps.Blob == nil || cfg.AuthMode != "token" {
		return nil, errors.New("native screening dependencies and authentication required")
	}
	s, e := store.New(db, database)
	if e != nil {
		return nil, e
	}
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	router := gin.New()
	metrics := newServiceMetrics()
	router.Use(requestContextMiddleware(log), mysqlRequestLog(log, metrics), mysqlRecovery(log))
	// Bound requests and deadlines before decoding. Human authentication is
	// checked by each registered surface, never substituted by tenant headers.
	router.Use(func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		if c.Request.Body != nil {
			limit := int64((1 << 20) + 4096)
			if strings.HasPrefix(c.FullPath(), "/local/evidence/") {
				limit = (10 << 20) + 1
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	})
	router.Use(func(c *gin.Context) {
		path := c.FullPath()
		// Check the appropriate credential before querying tenant registration.
		if strings.HasPrefix(path, "/v1/tenants/") || path == "/v1/service-info" || strings.HasPrefix(path, "/v1/screening-provider/") {
			if len(cfg.ScreeningOperatorToken) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Screening-Operator-Token")), []byte(cfg.ScreeningOperatorToken)) != 1 {
				c.AbortWithStatusJSON(403, gin.H{"error": "screening_owner_requires_private_operator"})
				return
			}
			if subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+cfg.AuthToken)) != 1 {
				c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
				return
			}
		} else if strings.HasPrefix(path, "/internal/") {
			if len(cfg.ScreeningIntakeToken) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+cfg.ScreeningIntakeToken)) != 1 {
				c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
				return
			}
		} else if strings.HasPrefix(path, "/v1/investigator/") {
			raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
			if !ok {
				c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
				return
			}
			p, e := cfg.InvestigatorVerifier.VerifyPlatformSession(raw)
			if e != nil {
				c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
				return
			}
			if p.TenantID.String() != c.Param("tenantId") {
				c.AbortWithStatusJSON(403, gin.H{"error": "forbidden"})
				return
			}
		}
		if raw := c.Param("tenantId"); raw != "" {
			tenant, e := uuid.Parse(raw)
			if e != nil || tenant == uuid.Nil {
				c.AbortWithStatusJSON(400, gin.H{"error": "invalid tenant"})
				return
			}
			var active bool
			e = db.QueryRowContext(c.Request.Context(), "SELECT EXISTS(SELECT 1 FROM "+schema+".identity_tenants WHERE id=? AND active)", mysqlstore.UUID(tenant)).Scan(&active)
			if e != nil {
				c.AbortWithStatusJSON(503, gin.H{"error": "tenant registry unavailable"})
				return
			}
			if !active {
				c.AbortWithStatusJSON(403, gin.H{"error": "forbidden"})
				return
			}
			c.Request = c.Request.WithContext(store.WithTenant(c.Request.Context(), tenant))
		}
		c.Next()
	})
	router.GET("/healthz", func(c *gin.Context) { c.Status(200) })
	router.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if db.PingContext(ctx) != nil {
			c.Status(503)
			return
		}
		c.Status(200)
	})
	router.GET("/metrics", func(c *gin.Context) { c.JSON(200, metrics.Snapshot()) })
	svc := MySQLService(s, deps)
	authorize := func(r *http.Request) (identity.Principal, error) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			return identity.Principal{}, &identity.Error{Code: 401, Message: "Investigator sign-in is required."}
		}
		if _, e := cfg.InvestigatorVerifier.VerifyPlatformSession(raw); e != nil {
			return identity.Principal{}, &identity.Error{Code: 401, Message: "A valid platform session is required."}
		}
		p, e := cfg.InvestigatorVerifier.Verify(raw)
		if e != nil {
			return p, identity.Forbidden()
		}
		if e = s.CheckIdentity(r.Context(), p, false); e != nil {
			return p, e
		}
		*r = *r.WithContext(store.WithPrincipal(r.Context(), p))
		return p, nil
	}
	registerManualRoutes(router, authorize, svc, deps.Capabilities)
	registerMySQLInvestigator(router, s, cfg.InvestigatorVerifier, svc, authorize)
	mountScreeningRoutes(router, cfg, svc, deps.Capabilities)
	if files, ok := deps.Blob.(interface{ Mount(*gin.Engine) }); ok {
		files.Mount(router)
	}
	mountMySQLDocs(router)
	return router, nil
}
