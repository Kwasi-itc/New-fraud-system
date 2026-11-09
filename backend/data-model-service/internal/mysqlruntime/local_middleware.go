package mysqlruntime

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type localAuthorityKey struct{}
type localIDs struct{}

func (localIDs) New() uuid.UUID { return uuid.New() }

type localClock struct{}

func (localClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func localAuthority(ctx context.Context, _ *sql.Tx) error {
	if ctx.Value(localAuthorityKey{}) != true {
		return errors.New("local development authority required")
	}
	return ctx.Err()
}
func localTenantAuthority(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	if id == uuid.Nil {
		return errors.New("nonzero tenant required")
	}
	return localAuthority(ctx, tx)
}

func localRequestMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		timeout := 10 * time.Second
		if c.Request.Method == "DELETE" && c.FullPath() == "/v1/tables/:tableId" {
			timeout = 70 * time.Second
		}
		if (c.Request.Method == "POST" && (c.FullPath() == "/v1/tenants/:tenantId/data-model/import" || c.FullPath() == "/v1/tenants/:tenantId/tables" || c.FullPath() == "/v1/tables/:tableId/fields")) || ((c.Request.Method == "PATCH" || c.Request.Method == "DELETE") && c.FullPath() == "/v1/fields/:fieldId") {
			timeout = 70 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.WithValue(c.Request.Context(), localAuthorityKey{}, true), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	}
}
func localAuthorMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+secret)) != 1 {
			c.AbortWithStatus(403)
			return
		}
		ctx, cancel := context.WithTimeout(context.WithValue(c.Request.Context(), localAuthorityKey{}, true), 60*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	}
}
