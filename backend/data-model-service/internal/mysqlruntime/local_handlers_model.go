package mysqlruntime

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func localModelHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("tenantId"))
		if err != nil || id == uuid.Nil {
			c.JSON(400, gin.H{"error": "invalid_tenant"})
			return
		}
		handler, err := NewModelEndpoint(db, "fraud", id, localMaxRows, localMaxBytes, localTenantAuthority)
		if err != nil {
			c.JSON(503, gin.H{"error": "model_unavailable"})
			return
		}
		handler(c)
	}
}
