package httpapi

import (
	"errors"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func clientAccessError(c *gin.Context, err error) {
	code, message := auth.HTTPError(err)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		code = 409
		message = "Client or inbox does not belong to this tenant."
	}
	if code == 500 {
		code = 503
		message = "Case access is temporarily unavailable."
	}
	c.AbortWithStatusJSON(code, gin.H{"error": message})
}
func clientCaseRead(db *pgxpool.Pool, human gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := auth.PrincipalFromContext(c)
		if p == nil || !p.IsClient() {
			human(c)
			return
		}
		if db == nil {
			c.AbortWithStatus(503)
			return
		}
		client, err := uuid.Parse(p.Subject)
		if err != nil {
			c.AbortWithStatus(403)
			return
		}
		repo := store.ClientAccessRepository{DB: db}
		if raw := c.Param("caseId"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				c.AbortWithStatus(400)
				return
			}
			item, err := repo.GetCase(c.Request.Context(), p.TenantID, client, id)
			if err != nil {
				clientAccessError(c, err)
				return
			}
			c.JSON(200, gin.H{"case": item})
			return
		}
		after := uuid.Nil
		if raw := c.Query("after"); raw != "" {
			after, err = uuid.Parse(raw)
			if err != nil {
				c.AbortWithStatus(400)
				return
			}
		}
		page, err := repo.ListCases(c.Request.Context(), p.TenantID, client, after)
		if err != nil {
			clientAccessError(c, err)
			return
		}
		c.JSON(200, page)
	}
}
func registerClientInboxAccess(r *auth.Router, db *pgxpool.Pool) {
	base := "/v1/tenants/:tenantId/clients/:clientId/inboxes"
	policy := auth.Policy{Capability: "inboxes:manage", PrincipalType: auth.PrincipalUser}
	handler := func(c *gin.Context) {
		p := auth.PrincipalFromContext(c)
		client, err := uuid.Parse(c.Param("clientId"))
		if err != nil || client == uuid.Nil {
			c.AbortWithStatus(400)
			return
		}
		if db == nil {
			c.AbortWithStatus(503)
			return
		}
		repo := store.ClientAccessRepository{DB: db}
		if c.Request.Method == "GET" {
			inboxes, err := repo.ListInboxes(c.Request.Context(), *p, client)
			if err != nil {
				clientAccessError(c, err)
				return
			}
			c.JSON(200, gin.H{"inbox_ids": inboxes})
			return
		}
		inbox, err := uuid.Parse(c.Param("inboxId"))
		if err != nil || inbox == uuid.Nil {
			c.AbortWithStatus(400)
			return
		}
		if err := repo.SetInbox(c.Request.Context(), *p, client, inbox, c.Request.Method == "PUT"); err != nil {
			clientAccessError(c, err)
			return
		}
		c.Status(204)
	}
	r.GET(base, policy, handler)
	r.PUT(base+"/:inboxId", policy, handler)
	r.DELETE(base+"/:inboxId", policy, handler)
}
