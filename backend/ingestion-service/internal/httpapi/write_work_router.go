package httpapi

import (
	"bytes"
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"strings"
	"time"
)

// NewWriteWorkRouter composes isolated write/work and bounded read capabilities.
// Administrative routes and production activation remain separate.
func NewWriteWorkRouter(logger *slog.Logger, cfg RouterConfig, writer handlers.IngestWriter, reader handlers.IngestReader, uploads handlers.UploadService, deferred handlers.DeferredService, bodyBytes int, bind func(context.Context, string) context.Context, ready func(context.Context) error) (*gin.Engine, error) {
	if cfg.AuthMode != "token" || cfg.AuthToken == "" || cfg.WritePathConcurrencyLimit < 1 || writer == nil || reader == nil || uploads == nil || deferred == nil || bind == nil || ready == nil || bodyBytes < 1 || bodyBytes > 1<<20 {
		return nil, errors.New("complete authenticated bounded write/work capabilities required")
	}
	r := gin.New()
	r.Use(requestContextMiddleware(logger), gin.Recovery(), corsMiddleware(cfg.AllowedOrigins))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready", "capabilities": []string{"write", "work", "read", "aggregate"}})
	})
	v := r.Group("/v1")
	v.Use(authMiddleware(AuthConfig{Mode: cfg.AuthMode, Token: cfg.AuthToken}))
	v.Use(func(c *gin.Context) {
		token, _ := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		ctx, cancel := context.WithTimeout(bind(c.Request.Context(), token), 15*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		if c.Request.Body != nil {
			data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(bodyBytes+(64<<10)+1)))
			if err != nil {
				c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"code": "bad_parameter", "message": "cannot read request"}})
				return
			}
			if len(data) > bodyBytes+(64<<10) {
				c.AbortWithStatusJSON(413, gin.H{"error": gin.H{"code": "request_limit", "message": "request exceeds configured limits"}})
				return
			}
			// The CSV route parses bounded multipart input. Every other body
			// uses JSON; caller Content-Type cannot bypass this preflight.
			isCSV := c.Request.Method == "POST" && strings.HasSuffix(c.FullPath(), "/csv")
			if !isCSV && len(data) > 0 && !validBoundedJSON(data, 64) {
				c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"code": "bad_parameter", "message": "one JSON document with at most 64 levels is required"}})
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(data))
		}
		c.Next()
	})
	limiter := make(chan struct{}, cfg.WritePathConcurrencyLimit)
	h := handlers.NewIngestHandler(service.IngestService{}, handlers.IngestHandlerConfig{WriteBackend: writer, ReadBackend: reader, ReadQueryConcurrencyLimit: 4, AggregateQueryConcurrencyLimit: 2, AggregateQueryTimeout: 5 * time.Second, WritePathLimiter: limiter, WritePathOverloadMode: cfg.WritePathOverloadMode, DeferredIngestService: deferred})
	u := handlers.NewUploadLogHandler(uploads, handlers.UploadLogHandlerConfig{WritePathLimiter: limiter})
	d := handlers.NewDeferredIngestHandler(deferred)
	v.GET("/tenants/:tenantId/records/:objectType/:objectId", h.GetRecord)
	v.GET("/tenants/:tenantId/records/:objectType", h.ListRecords)
	v.GET("/tenants/:tenantId/records/:objectType/search", h.QueryRecords)
	v.POST("/tenants/:tenantId/query/aggregate", h.AggregateRecords)
	v.POST("/tenants/:tenantId/ingest/:objectType", h.PostIngest)
	v.PATCH("/tenants/:tenantId/ingest/:objectType", h.PatchIngest)
	v.POST("/tenants/:tenantId/ingest/:objectType/batch", h.PostBatchIngest)
	v.PATCH("/tenants/:tenantId/ingest/:objectType/batch", h.PatchBatchIngest)
	v.POST("/tenants/:tenantId/ingest/:objectType/csv", u.CreateCSV)
	v.GET("/tenants/:tenantId/ingest/:objectType/upload-logs", u.List)
	v.GET("/upload-logs/:uploadLogId", u.Get)
	v.GET("/deferred-ingests/:deferredIngestId", d.Get)
	return r, nil
}
