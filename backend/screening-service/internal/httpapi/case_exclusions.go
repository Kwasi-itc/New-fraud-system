package httpapi

import (
	"context"
	"strconv"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerCaseExclusions(router *gin.Engine, db *pgxpool.Pool, verifier *identity.Verifier) {
	authorize := manualAuthorization(db, verifier, db != nil)
	registerCaseExclusionRoutes(router, authorize, func(ctx context.Context, p identity.Principal, tenant, caseID uuid.UUID, operation, cursor string, limit int) (store.CaseExclusionPage, error) {
		return store.ReadCaseExclusions(ctx, db, p, tenant, caseID, operation, cursor, limit)
	})
}

type caseExclusionReader func(context.Context, identity.Principal, uuid.UUID, uuid.UUID, string, string, int) (store.CaseExclusionPage, error)

func registerCaseExclusionRoutes(router *gin.Engine, authorize manualAuthorizer, read caseExclusionReader) {
	router.GET("/v1/investigator/tenants/:tenantId/cases/:caseId/exclusions", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		p, err := authorize(c.Request)
		if err != nil {
			manualError(c, err)
			return
		}
		tenant, err := uuid.Parse(c.Param("tenantId"))
		if err != nil {
			manualError(c, identity.Invalid("Invalid tenant."))
			return
		}
		caseID, err := uuid.Parse(c.Param("caseId"))
		if err != nil {
			manualError(c, identity.Invalid("Invalid case."))
			return
		}
		limit := 20
		if raw := c.Query("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil {
				manualError(c, identity.Invalid("Invalid history limit."))
				return
			}
		}
		if limit < 1 || limit > 50 {
			manualError(c, identity.Invalid("History limit must be between 1 and 50."))
			return
		}
		out, err := read(c.Request.Context(), p, tenant, caseID, c.Query("operation_id"), c.Query("cursor"), limit)
		if err != nil {
			code, message := identity.HTTPError(err)
			if code >= 500 {
				message = "Exclusion history is temporarily unavailable."
			}
			c.JSON(code, gin.H{"error": message})
			return
		}
		c.JSON(200, out)
	})
}
