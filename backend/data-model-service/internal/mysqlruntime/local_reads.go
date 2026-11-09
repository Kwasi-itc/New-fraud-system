package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"

	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func adaptLocalList[A, B any](values []A, err error, adapt func(A) B, key string) (any, error) {
	if err != nil {
		return nil, err
	}
	result := make([]B, len(values))
	for i, v := range values {
		result[i] = adapt(v)
	}
	return gin.H{key: result}, nil
}

type localMetadataRead func(context.Context, store.MetadataRepositories, uuid.UUID) (any, error)

func mountLocalRead(router *gin.Engine, db *sql.DB, path, param, resource string, fn localMetadataRead) {
	router.GET(path, func(c *gin.Context) {
		id, err := uuid.Parse(c.Param(param))
		if err != nil || id == uuid.Nil {
			c.JSON(400, gin.H{"error": gin.H{"code": "bad_parameter"}})
			return
		}
		result, err := readLocalMetadata(c.Request.Context(), db, id, resource, fn)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		if err != nil {
			c.JSON(503, gin.H{"error": gin.H{"code": "read_unavailable"}})
			return
		}
		c.JSON(200, result)
	})
}

func registerLocalReads(router *gin.Engine, db *sql.DB) {
	registerLocalTableReads(router, db)
	registerLocalFieldReads(router, db)
	registerLocalRelationshipReads(router, db)
	registerLocalHistoryReads(router, db)
}

// readLocalMetadata keeps tenant resolution and all reads in one bounded snapshot.
func readLocalMetadata(ctx context.Context, db *sql.DB, id uuid.UUID, resource string, fn localMetadataRead) (any, error) {
	var result any
	err := mysqlstore.Snapshot(ctx, db, func(tx *sql.Tx) error {
		if err := localAuthority(ctx, tx); err != nil {
			return err
		}
		scope := id
		// Resource table names are fixed at registration, never supplied by HTTP.
		if resource != "" {
			if err := tx.QueryRowContext(ctx, "SELECT tenant_id FROM fraud."+resource+" WHERE id=?"+localActiveResource(resource), mysqlstore.UUID(id)).Scan((*mysqlstore.UUID)(&scope)); err != nil {
				return err
			}
		}
		r, err := store.NewMetadataRepositories(tx, "fraud", scope, 1000, 4<<20)
		if err != nil {
			return err
		}
		if _, err := r.Tenants.GetByID(ctx, scope); err != nil {
			return err
		}
		result, err = fn(ctx, r, id)
		return err
	})

	return result, err
}

func localActiveResource(resource string) string {
	if resource == "data_model_tables" || resource == "data_model_fields" {
		return " AND NOT archived"
	}
	return ""
}
