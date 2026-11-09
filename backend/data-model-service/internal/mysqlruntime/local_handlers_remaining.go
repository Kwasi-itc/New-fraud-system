package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Remaining routes share private author authentication and request budgets.
func registerLocalRemaining(r *gin.Engine, p *LocalCreation) {
	bind := func(kind, param string) gin.HandlerFunc {
		return func(c *gin.Context) {
			id, key, ok := localMutationIDs(c, param)
			if !ok {
				return
			}
			input := relationshipCommand{Kind: kind, ID: id}
			var body any
			switch kind {
			case "link_create":
				body = &input.Link
			case "pivot_create":
				body = &input.Pivot
			case "options":
				body = &input.Options
			case "navigation_create":
				body = &input.Navigation
			}
			if body != nil {
				if decodeLocalPatch(c, body) != nil {
					c.JSON(400, gin.H{"error": "invalid_request"})
					return
				}
			}
			dry := c.Query("dry_run")
			if dry != "" && dry != "true" && dry != "false" {
				c.JSON(400, gin.H{"error": "invalid_dry_run"})
				return
			}
			if dry != "" && kind != "link_delete" && kind != "pivot_delete" {
				c.JSON(400, gin.H{"error": "dry_run is only supported for link and pivot deletion"})
				return
			}
			input.Dry = dry == "true"
			response, err := p.relationship(c.Request.Context(), input, key)
			if err != nil {
				localUpdateError(c, err)
				return
			}
			status := 200
			if kind == "link_create" || kind == "pivot_create" || kind == "navigation_create" {
				status = 201
			}
			if kind == "navigation_delete" {
				c.Status(204)
				return
			}
			c.Data(status, "application/json", response)
		}
	}
	r.POST("/v1/tenants/:tenantId/links", bind("link_create", "tenantId"))
	r.DELETE("/v1/links/:linkId", bind("link_delete", "linkId"))
	r.POST("/v1/tenants/:tenantId/pivots", bind("pivot_create", "tenantId"))
	r.DELETE("/v1/pivots/:pivotId", bind("pivot_delete", "pivotId"))
	r.PUT("/v1/tables/:tableId/options", bind("options", "tableId"))
	r.POST("/v1/tables/:tableId/navigation-options", bind("navigation_create", "tableId"))
	r.DELETE("/v1/navigation-options/:navigationOptionId", bind("navigation_delete", "navigationOptionId"))
	index := func(action string) gin.HandlerFunc {
		return func(c *gin.Context) {
			param := "tenantId"
			if action == "retry" {
				param = "jobId"
			}
			id, e := uuid.Parse(c.Param(param))
			if e != nil || id == uuid.Nil {
				c.JSON(400, gin.H{"error": "invalid_id"})
				return
			}
			tenant := id
			if action == "retry" {
				var t mysqlstore.UUID
				if e := p.Target.QueryRowContext(c.Request.Context(), "SELECT tenant_id FROM fraud.data_model_index_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&t); e != nil {
					localUpdateError(c, e)
					return
				}
				tenant = uuid.UUID(t)
			}
			queue, e := localIndexQueue(p.Target)
			if e != nil {
				localUpdateError(c, e)
				return
			}
			app, e := store.NewIndexJobApplication(p.Target, "fraud", tenant, localMaxRows, localMaxBytes, localTenantAuthority, localTenantAuthority, queue, localIDs{}, localClock{})
			if e != nil {
				localUpdateError(c, e)
				return
			}
			h := handlers.NewStagedIndexJobHandler(app)
			switch action {
			case "create":
				h.Create(c)
			case "scoped":
				h.ListScoped(c)
			case "retry":
				h.Retry(c)
			}
		}
	}
	r.POST("/v1/tenants/:tenantId/index-jobs", index("create"))
	r.GET("/v1/tenants/:tenantId/index-jobs/scoped", index("scoped"))
	r.POST("/v1/index-jobs/:jobId/retry", index("retry"))
	r.GET("/v1/admin/reconcile", func(c *gin.Context) {
		scope := uuid.Nil
		if raw := c.Query("tenant_id"); raw != "" {
			var e error
			scope, e = uuid.Parse(raw)
			if e != nil || scope == uuid.Nil {
				c.JSON(400, gin.H{"error": "invalid_tenant_id"})
				return
			}
		}
		report, e := p.reconcileLocal(c.Request.Context(), scope)
		if e != nil {
			localUpdateError(c, e)
			return
		}
		c.JSON(200, gin.H{"report": report})
	})
}
