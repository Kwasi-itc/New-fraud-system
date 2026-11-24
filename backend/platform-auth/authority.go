package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string     { return e.Message }
func Invalid(message string) error { return &Error{400, message} }
func Conflict() error {
	return &Error{409, "Resource changed or operation conflicts; refresh before retrying."}
}
func Forbidden() error { return &Error{403, "Current capability and resource access are required."} }
func HTTPError(err error) (int, string) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.Message
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return 409, "Operation conflicts with an existing resource."
	}
	return 500, "Operation could not be completed. Retry with the same operation ID."
}

// CheckCapability calls the identity-owned current-authority contract.
// Only mutations request locks, and their caller must supply its existing transaction.
func CheckCapability(ctx context.Context, q Queryer, p Principal, capability string, lock bool) error {
	if !p.IsHuman() || !p.Can(capability) || p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.AuthzVersion < 1 {
		return Forbidden()
	}
	if _, ok := LookupCapability(capability); !ok {
		return Forbidden()
	}
	var allowed bool
	if err := q.QueryRow(ctx, `SELECT identity.authorize_capability_v1($1,$2,$3,$4,$5,$6)`, p.Subject, p.TenantID, p.SessionID, p.AuthzVersion, capability, lock).Scan(&allowed); err != nil {
		return &Error{503, "Current identity authority is unavailable."}
	}
	if !allowed {
		return Forbidden()
	}
	return nil
}
func rollback(ctx context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}
