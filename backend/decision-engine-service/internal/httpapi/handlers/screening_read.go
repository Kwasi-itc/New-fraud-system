package handlers

import (
	"context"
	"errors"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ScreeningReadHandler struct{ Store store.ScreeningReadRepository }

func (h ScreeningReadHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	p := platformauth.PrincipalFromContext(c)
	if p == nil || !p.IsHuman() {
		c.JSON(403, gin.H{"error": "Human operator required."})
		return
	}
	actor := workflow.AuthoringActor{UserID: p.Subject, SessionID: p.SessionID.String(), Version: p.AuthzVersion}

	for _, value := range []string{c.Param("tenantId"), c.Param("decisionId"), actor.UserID, actor.SessionID} {
		if id, err := uuid.Parse(value); err != nil || id == uuid.Nil {
			c.JSON(400, gin.H{"error": "Invalid resource or identity."})
			return
		}
	}
	if actor.Version < 1 {
		c.JSON(403, gin.H{"error": "Screening access denied."})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	items, err := h.Store.List(ctx, c.Param("tenantId"), c.Param("decisionId"), actor)
	if err != nil {
		if errors.Is(err, workflow.ErrAuthoringDenied) {
			c.JSON(403, gin.H{"error": "Screening access denied."})
		} else {
			c.JSON(503, gin.H{"error": "Screening results are unavailable."})
		}
		return
	}
	c.JSON(200, gin.H{"screening_executions": items})
}
