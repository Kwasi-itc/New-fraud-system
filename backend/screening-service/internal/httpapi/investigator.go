package httpapi

import (
	"context"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerInvestigator(router *gin.Engine, db *pgxpool.Pool, verifier *identity.Verifier, svc service.ScreeningService) {
	handler := func(c *gin.Context) {
		identity.Serve(c.Writer, c.Request, verifier, func(p identity.Principal, resource string, cmd *identity.Command) (identity.State, error) {
			tenant, e := uuid.Parse(c.Param("tenantId"))
			if e != nil {
				return identity.State{}, identity.Invalid("Invalid tenant.")
			}
			caseID, e := uuid.Parse(c.Param("caseId"))
			if e != nil {
				return identity.State{}, identity.Invalid("Invalid case.")
			}
			state, err := identity.Run(c.Request.Context(), db, p, tenant, caseID, resource, "screening", cmd, func(ctx context.Context, tx pgx.Tx, command *identity.Command) (identity.State, error) {
				return svc.InvestigatorReview(ctx, store.RepositoriesInTransaction(tx), tenant.String(), resource, p.Subject, command)
			})
			return state, err
		})
	}
	router.GET("/v1/investigator/tenants/:tenantId/cases/:caseId/review", handler)
	router.POST("/v1/investigator/tenants/:tenantId/cases/:caseId/review", handler)
}
