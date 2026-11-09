package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/gin-gonic/gin"
)

// NewModelReadHandler exposes only the snapshot read operation. No mutation
// services are constructed or registered by this bounded composition.
func NewModelReadHandler(repository ports.DataModelSnapshotRepository) (gin.HandlerFunc, error) {
	svc, err := service.NewSnapshotDataModelReadService(repository)
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		tenant, ok := parseUUIDParam(c, "tenantId")
		if !ok {
			return
		}
		model, err := svc.Get(c.Request.Context(), tenant)
		if err != nil {
			var auth *access.Error
			switch {
			case errors.As(err, &auth):
				c.AbortWithStatusJSON(auth.Status, gin.H{"error": gin.H{"code": "authorization_failed", "message": auth.Message}})
			case errors.Is(err, sql.ErrNoRows):
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			default:
				// SQL errors and schema details must not be returned to callers.
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "model_unavailable"}})
			}
			return
		}
		writeConditionalModel(c, gin.H{"data_model": dto.AdaptAssembledDataModel(model.Model, model.RevisionID, model.Tenant.Status)})
	}, nil
}
