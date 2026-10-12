package platform

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	caller, err := s.Resolve(ctx, raw)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT id,created_at,expires_at,idle_until,id=$3
 FROM identity.sessions WHERE user_id=$1 AND tenant_id=$2 AND id>$4
 AND revoked_at IS NULL AND expires_at>now() AND idle_until>now()
 ORDER BY id LIMIT 101`, caller.UserID, caller.TenantID, caller.ID, after)
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
	if (all && target != uuid.Nil) || (!all && target == uuid.Nil) {
		return false, ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	caller, err := session(ctx, tx, raw)
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM identity.users WHERE id=$1 FOR SHARE`, caller.UserID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `SELECT user_id FROM identity.memberships WHERE user_id=$1 AND tenant_id=$2 FOR UPDATE`, caller.UserID, caller.TenantID); err != nil {
		return false, err
	}
	// Revalidate after waiting for suspension or a competing session operation.
	caller, err = session(ctx, tx, raw)
	if err != nil {
		return false, err
	}
	var live bool
	if err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() FROM identity.sessions WHERE id=$1 FOR UPDATE`, caller.ID).Scan(&live); err != nil {
		return false, err
	}
	if !live {
		return false, ErrDenied
	}
	if !all {
		var owned bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.sessions WHERE id=$1 AND user_id=$2 AND tenant_id=$3)`, target, caller.UserID, caller.TenantID).Scan(&owned); err != nil {
			return false, err
		}
		if !owned {
			return false, ErrDenied
		}
	}
	result, err := tx.Exec(ctx, `UPDATE identity.sessions SET revoked_at=now() WHERE user_id=$1 AND tenant_id=$2 AND revoked_at IS NULL AND ($3 OR id=$4)`, caller.UserID, caller.TenantID, all, target)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() > 0 {
		action := "session.revoked"
		auditTarget := target
		if all {
			action = "sessions.revoked_all"
			auditTarget = caller.ID
		}
		if err = audit(ctx, tx, caller.TenantID, caller.UserID.String(), action, auditTarget, map[string]any{"count": result.RowsAffected()}); err != nil {
			return false, err
		}
	}
	return all || target == caller.ID, tx.Commit(ctx)
}
