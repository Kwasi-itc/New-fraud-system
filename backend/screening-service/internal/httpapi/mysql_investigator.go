package httpapi

import (
	"context"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerMySQLInvestigator(router *gin.Engine, s *store.Store, verifier *identity.Verifier, svc service.ScreeningService, authorize manualAuthorizer) {
	handler := func(c *gin.Context) {
		identity.Serve(c.Writer, c.Request, verifier, func(p identity.Principal, resource string, cmd *identity.Command) (identity.State, error) {
			tenant, e1 := uuid.Parse(c.Param("tenantId"))
			caseID, e2 := uuid.Parse(c.Param("caseId"))
			if e1 != nil || e2 != nil {
				return identity.State{}, identity.Invalid("Invalid tenant or case.")
			}
			return s.Review(c.Request.Context(), p, tenant, caseID, resource, cmd, func(ctx context.Context, r *store.Store, command *identity.Command) (identity.State, error) {
				return svc.InvestigatorReview(ctx, ports.MutationStore(r), tenant.String(), resource, p.Subject, command)
			})
		})
	}
	router.GET("/v1/investigator/tenants/:tenantId/cases/:caseId/review", handler)
	router.POST("/v1/investigator/tenants/:tenantId/cases/:caseId/review", handler)
	registerCaseExclusionRoutes(router, authorize, s.CaseExclusions)
}
