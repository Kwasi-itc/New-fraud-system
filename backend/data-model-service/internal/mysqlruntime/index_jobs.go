package mysqlruntime

import (
	"database/sql"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// IndexJobEndpoints exposes only the bounded preparation API. Authentication is
// supplied by the trusted caller; read/write callbacks recheck it in local SQL.
type IndexJobEndpoints struct{ Scoped, Create, Retry gin.HandlerFunc }

func NewIndexJobEndpoints(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, read store.ModelReadAuthorizer, write store.MutationAuthorizer, queue *store.IndexDeliveryStore, ids ports.IDGenerator, clock ports.Clock) (IndexJobEndpoints, error) {
	app, err := store.NewIndexJobApplication(db, database, tenant, maxRows, maxBytes, read, write, queue, ids, clock)
	if err != nil {
		return IndexJobEndpoints{}, err
	}
	h := handlers.NewStagedIndexJobHandler(app)
	bind := func(next gin.HandlerFunc, pathTenant bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			if pathTenant {
				id, err := uuid.Parse(c.Param("tenantId"))
				if err != nil || id == uuid.Nil {
					c.AbortWithStatus(http.StatusBadRequest)
					return
				}
				if id != tenant {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxBytes))
			next(c)
		}
	}
	return IndexJobEndpoints{Scoped: bind(h.ListScoped, true), Create: bind(h.Create, true), Retry: bind(h.Retry, false)}, nil
}
