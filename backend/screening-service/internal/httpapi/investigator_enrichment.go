package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerInvestigatorEnrichment(router *gin.Engine, db *pgxpool.Pool, verifier *identity.Verifier, svc service.ScreeningService) {
	router.POST("/v1/investigator/tenants/:tenantId/cases/:caseId/matches/:matchId/enrich", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		principal, err := manualAuthorization(db, verifier, true)(c.Request)
		if err != nil {
			manualError(c, err)
			return
		}
		tenant, e := uuid.Parse(c.Param("tenantId"))
		caseID, e2 := uuid.Parse(c.Param("caseId"))
		matchID := c.Param("matchId")
		if e != nil || e2 != nil || tenant == uuid.Nil || caseID == uuid.Nil || matchID == "" || len(matchID) > 200 {
			manualError(c, identity.Invalid("Invalid evidence reference."))
			return
		}
		if c.ContentType() != "application/json" {
			c.JSON(415, gin.H{"error": "Expected application/json."})
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
		var body map[string]any
		if decoder.Decode(&body) != nil || body == nil || len(body) != 0 || decoder.Decode(new(any)) != io.EOF {
			manualError(c, identity.Invalid("Expected an empty enrichment request."))
			return
		}
		check := func(ctx context.Context, tx pgx.Tx, _ *identity.Command) (identity.State, error) {
			return svc.InvestigatorReview(ctx, store.RepositoriesInTransaction(tx), tenant.String(), matchID, principal.Subject, nil)
		}
		if _, err = identity.Run(c.Request.Context(), db, principal, tenant, caseID, matchID, "screening", nil, check); err != nil {
			manualError(c, err)
			return
		}
		scope, err := provideraccess.FromPrincipal(principal)
		if err != nil {
			manualError(c, err)
			return
		}
		ctx := provideraccess.WithScope(c.Request.Context(), scope)
		original, supplement, err := svc.PrepareMatchEnrichment(ctx, tenant.String(), matchID)
		if err != nil {
			manualError(c, err)
			return
		}
		// Provider I/O has finished. Recheck identity/case/inbox/link under the usual
		// locks and merge with the latest review state in the same transaction.
		_, err = identity.Run(ctx, db, principal, tenant, caseID, matchID, "screening", nil, func(ctx context.Context, tx pgx.Tx, _ *identity.Command) (identity.State, error) {
			repo := store.RepositoriesInTransaction(tx)
			if _, err := svc.ApplyMatchEnrichment(ctx, repo, original, supplement); err != nil {
				return identity.State{}, err
			}
			return svc.InvestigatorReview(ctx, repo, tenant.String(), matchID, principal.Subject, nil)
		})
		if err != nil {
			manualError(c, err)
			return
		}
		c.JSON(200, gin.H{"enriched": true})
	})
}
