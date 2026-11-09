package mysqlruntime

import (
	"database/sql"
	"errors"
	"log/slog"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func (h *localAuthorHandler) CreateTable(c *gin.Context) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-c.Request.Context().Done():
		c.JSON(503, gin.H{"error": "authoring_busy"})
		return
	}
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_tenant"})
		return
	}
	var body dto.CreateTableRequest
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return
	}
	if err := datamodel.ValidateTableCreate(body.Name); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := datamodel.ValidateSemanticType(body.SemanticType); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	op := uuid.NewSHA1(id, []byte("local-create-table/v1/"+datamodel.NormalizeName(body.Name)))
	if supplied := c.GetHeader("Idempotency-Key"); supplied != "" {
		op, err = uuid.Parse(supplied)
		if err != nil || op == uuid.Nil {
			c.JSON(400, gin.H{"error": "Idempotency-Key must be a nonzero UUID"})
			return
		}
	}
	c.Header("Idempotency-Key", op.String())
	record, err := h.worker.Create(c.Request.Context(), service.CreateTableInput{TenantID: id, Name: body.Name, Alias: body.Alias, Description: body.Description, SemanticType: body.SemanticType}, op)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "local table creation failed", "operation", op, "error", err)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		var mysqlErr *driver.MySQLError
		if errors.Is(err, store.ErrPublicationOperationConflict) || errors.Is(err, store.ErrPhysicalConflict) || (errors.As(err, &mysqlErr) && mysqlErr.Number == 1062) {
			c.JSON(409, gin.H{"error": gin.H{"code": "table_creation_conflict", "operation_id": op}})
			return
		}
		c.JSON(503, gin.H{"error": gin.H{"code": "table_creation_incomplete", "message": "Creation could not be confirmed. Retry the identical request with the same Idempotency-Key.", "operation_id": op}})
		return
	}
	c.JSON(201, gin.H{"table": dto.AdaptTable(record)})
}
