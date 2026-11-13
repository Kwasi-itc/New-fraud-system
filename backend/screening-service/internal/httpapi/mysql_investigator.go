package httpapi

import (
	"context"
	"encoding/json"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
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
	refinement := func(c *gin.Context) {
		p, e := authorize(c.Request)
		if e != nil {
			code, message := identity.HTTPError(e)
			c.JSON(code, gin.H{"error": message})
			return
		}
		t, e1 := uuid.Parse(c.Param("tenantId"))
		caseID, e2 := uuid.Parse(c.Param("caseId"))
		source, e3 := uuid.Parse(c.Param("screeningId"))
		if e1 != nil || e2 != nil || e3 != nil {
			c.JSON(400, gin.H{"error": "Invalid screening context."})
			return
		}
		var out any
		if c.Request.Method == "GET" {
			out, e = s.RefinementContext(c.Request.Context(), p, t, caseID, source)
		} else {
			var cmd store.RefinementCommand
			d := json.NewDecoder(io.LimitReader(c.Request.Body, 8193))
			d.DisallowUnknownFields()
			if d.Decode(&cmd) != nil {
				c.JSON(400, gin.H{"error": "Invalid refinement command."})
				return
			}
			var tail any
			if d.Decode(&tail) != io.EOF {
				c.JSON(400, gin.H{"error": "Invalid refinement command."})
				return
			}
			var id string
			id, e = s.ApplyRefinement(c.Request.Context(), p, t, caseID, source, cmd)
			out = gin.H{"screening_id": id}
		}
		if e != nil {
			code, message := identity.HTTPError(e)
			c.JSON(code, gin.H{"error": message})
			return
		}
		c.JSON(200, out)
	}
	path := "/v1/investigator/tenants/:tenantId/cases/:caseId/screenings/:screeningId/refinement"
	router.GET(path, refinement)
	router.POST(path, refinement)
}
