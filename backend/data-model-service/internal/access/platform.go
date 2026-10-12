// Package access carries the verified human request into owner transactions.
package access

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }
func Denied() error            { return &Error{403, "Data Model permission and tenant access are required."} }

type Queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Grant struct {
	Principal investigator.Principal
	Write     bool
	Kind      string
	ID        uuid.UUID
}
type contextKey struct{}

func WithGrant(ctx context.Context, g Grant) context.Context {
	return context.WithValue(ctx, contextKey{}, g)
}
func FromContext(ctx context.Context) (Grant, bool) {
	g, ok := ctx.Value(contextKey{}).(Grant)
	return g, ok
}

// Only fixed SQL is selected here. Resource ownership is resolved from owner
// tables, never a client-provided tenant header or SQL identifier.
var ownership = map[string]string{
	"tableId":            "SELECT tenant_id FROM core.model_tables WHERE id=$1",
	"fieldId":            "SELECT tenant_id FROM core.model_fields WHERE id=$1",
	"enumValueId":        "SELECT tenant_id FROM core.field_enum_values WHERE id=$1",
	"linkId":             "SELECT tenant_id FROM core.model_links WHERE id=$1",
	"pivotId":            "SELECT tenant_id FROM core.model_pivots WHERE id=$1",
	"navigationOptionId": "SELECT tenant_id FROM core.navigation_options WHERE id=$1",
	"jobId":              "SELECT tenant_id FROM core.index_jobs WHERE id=$1",
}

func Check(ctx context.Context, db Queryer, g Grant, lock bool) error {
	p := g.Principal
	if p.SessionID == uuid.Nil || p.TenantID == uuid.Nil || g.ID == uuid.Nil || p.AuthzVersion < 1 {
		return Denied()
	}
	var allowed bool
	if err := db.QueryRow(ctx, "SELECT identity.authorize_data_model_v1($1,$2,$3,$4,$5,$6)", p.Subject, p.TenantID, p.SessionID, p.AuthzVersion, g.Write, lock).Scan(&allowed); err != nil {
		return &Error{503, "Identity verification is temporarily unavailable."}
	}
	if !allowed {
		return Denied()
	}
	tenant := g.ID
	if g.Kind != "tenantId" {
		query, ok := ownership[g.Kind]
		if !ok {
			return Denied()
		}
		if err := db.QueryRow(ctx, query, g.ID).Scan(&tenant); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Denied()
			}
			return &Error{503, "Resource authorization is temporarily unavailable."}
		}
	}
	if tenant != p.TenantID {
		return Denied()
	}
	return nil
}
