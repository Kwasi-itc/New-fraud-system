// Package mysqlruntime provides explicitly staged owner compositions. The
// configured application continues to use PostgreSQL until cutover is approved.
package mysqlruntime

import (
	"database/sql"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// NewModelEndpoint binds a tenant, the shared database pool and mandatory trusted
// authority. Mount at GET /v1/tenants/:tenantId/data-model behind trusted caller
// authentication. The repository rejects a path tenant different from its scope.
// This constructor never selects a database from request headers or connects to
// a fallback database. Authority must be evaluated even for conditional requests.
func NewModelEndpoint(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize store.ModelReadAuthorizer) (gin.HandlerFunc, error) {
	r, err := store.NewAuthorizedPublishedModelReader(db, database, tenant, maxRows, maxBytes, authorize)
	if err != nil {
		return nil, err
	}
	handler, err := handlers.NewModelReadHandler(r)
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("tenantId"))
		if err != nil || id == uuid.Nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if id != tenant {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		handler(c)
	}, nil
}
