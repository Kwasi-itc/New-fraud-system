package platform

import (
	"context"
	"errors"
	"time"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SessionInfo never includes a cookie secret, digest or downstream token.
type SessionInfo struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IdleUntil time.Time `json:"idle_until"`
	Current   bool      `json:"current"`
}

func (s Store) Sessions(ctx context.Context, raw string, after uuid.UUID) ([]SessionInfo, error) {
	p, err := s.principalForSession(ctx, raw)
	if err != nil {
		return nil, err
	}
	return s.SessionsForPrincipal(ctx, p, after)
}

func (s Store) SessionsForPrincipal(ctx context.Context, p auth.Principal, after uuid.UUID) ([]SessionInfo, error) {
	if err := s.ValidatePrincipal(ctx, p); err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(p.Subject)
	if err != nil {
		return nil, ErrDenied
	}
	rows, err := s.DB.Query(ctx, `SELECT id,created_at,expires_at,idle_until,id=$3
 FROM identity.sessions WHERE user_id=$1 AND tenant_id=$2 AND id>$4
 AND revoked_at IS NULL AND expires_at>now() AND idle_until>now()
 ORDER BY id LIMIT 101`, userID, p.TenantID, p.SessionID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var item SessionInfo
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.ExpiresAt, &item.IdleUntil, &item.Current); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// RevokeSessions is self-service and scoped to the caller's active tenant.
// Exclusive membership locking serializes session creation, permission changes
// and competing revocations before session locks. Case writes hold SHARE on the
// same membership, so revocation commits after any already-authorized mutation.
func (s Store) RevokeSessions(ctx context.Context, raw string, target uuid.UUID, all bool) (bool, error) {
	p, err := s.principalForSession(ctx, raw)
	if err != nil {
		return false, err
	}
	return s.RevokeSessionsForPrincipal(ctx, p, target, all)
}

func (s Store) RevokeSessionsForPrincipal(ctx context.Context, p auth.Principal, target uuid.UUID, all bool) (bool, error) {
	if (all && target != uuid.Nil) || (!all && target == uuid.Nil) {
		return false, ErrDenied
	}
	userID, parseErr := uuid.Parse(p.Subject)
	if !p.IsHuman() || parseErr != nil || userID == uuid.Nil || p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.AuthzVersion < 1 {
		return false, ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var userActive bool
	if err = tx.QueryRow(ctx, `SELECT active FROM identity.users WHERE id=$1 FOR SHARE`, userID).Scan(&userActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrDenied
		}
		return false, err
	}
	if !userActive {
		return false, ErrDenied
	}
	var membershipActive bool
	var currentVersion int64
	if err = tx.QueryRow(ctx, `SELECT active,version FROM identity.memberships WHERE user_id=$1 AND tenant_id=$2 FOR UPDATE`, userID, p.TenantID).Scan(&membershipActive, &currentVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrDenied
		}
		return false, err
	}
	if !membershipActive || currentVersion != p.AuthzVersion {
		return false, ErrDenied
	}
	var live bool
	if err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() FROM identity.sessions WHERE id=$1 AND user_id=$2 AND tenant_id=$3 FOR UPDATE`, p.SessionID, userID, p.TenantID).Scan(&live); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrDenied
		}
		return false, err
	}
	if !live {
		return false, ErrDenied
	}
	if err := validatePrincipal(ctx, tx, p); err != nil {
		return false, err
	}
	if !all {
		var owned bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.sessions WHERE id=$1 AND user_id=$2 AND tenant_id=$3)`, target, userID, p.TenantID).Scan(&owned); err != nil {
			return false, err
		}
		if !owned {
			return false, ErrDenied
		}
	}
	result, err := tx.Exec(ctx, `UPDATE identity.sessions SET revoked_at=now() WHERE user_id=$1 AND tenant_id=$2 AND revoked_at IS NULL AND ($3 OR id=$4)`, userID, p.TenantID, all, target)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() > 0 {
		action := "session.revoked"
		auditTarget := target
		if all {
			action = "sessions.revoked_all"
			auditTarget = p.SessionID
		}
		if err = audit(ctx, tx, p.TenantID, userID.String(), action, auditTarget, map[string]any{"count": result.RowsAffected()}); err != nil {
			return false, err
		}
	}
	return all || target == p.SessionID, tx.Commit(ctx)
}
