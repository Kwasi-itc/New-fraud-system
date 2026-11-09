package httpapi

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi/handlers"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
	"strings"
	"time"
)

// NewWorkflowReceiptRouter exposes only the explicitly supplied owner capability.
// It does not select a database or claim that unrelated case APIs are available.
func NewWorkflowReceiptRouter(logger *slog.Logger, owner handlers.WorkflowReceiptOwner, auth AuthConfig, timeout time.Duration) (*gin.Engine, error) {
	if logger == nil || owner == nil || auth.Mode != "token" || len(auth.Token) < 32 || strings.TrimSpace(auth.Token) != auth.Token || strings.ContainsAny(auth.Token, "\r\n") || len(auth.TenantIDs) == 0 || timeout <= 0 || timeout > time.Minute {
		return nil, errors.New("authenticated workflow receipt owner configuration required")
	}
	for _, id := range auth.TenantIDs {
		if id == uuid.Nil {
			return nil, errors.New("nonzero workflow tenant required")
		}
	}
	r := gin.New()
	r.Use(requestContextMiddleware(logger), recoverRequest(), requestDeadline(timeout))
	g := r.Group("/internal/v1/workflow-receipts")
	g.Use(authMiddleware(auth))
	h := handlers.WorkflowReceiptHandler{Owner: owner}
	g.POST("/deliver", h.Deliver)
	g.POST("/reconcile", h.Reconcile)
	return r, nil
}
