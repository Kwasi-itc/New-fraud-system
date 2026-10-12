package investigator

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
)

// CheckIdentity uses the auth-owned versioned read contract. Domain mutations
// lock identity rows before case/inbox/resource rows; no network IO is involved.
func CheckIdentity(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, p Principal, lock bool) error {
	if p.SessionID == uuid.Nil {
		if os.Getenv("USER_REQUIRE_SESSION") == "true" {
			return Forbidden()
		}
		return nil
	}
	var allowed bool
	if err := db.QueryRow(ctx, `SELECT identity.authorize_case_v1($1,$2,$3,$4,$5,$6)`, p.Subject, p.TenantID, p.SessionID, p.AuthzVersion, p.Admin, lock).Scan(&allowed); err != nil {
		return &Error{503, "Identity verification is temporarily unavailable."}
	}
	if !allowed {
		return Forbidden()
	}
	return nil
}
