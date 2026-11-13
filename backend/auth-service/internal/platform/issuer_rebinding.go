package platform

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RebindIssuer is an explicit operator repair for a verified provider URL change.
// The caller must verify that both URLs identify the same provider realm and
// subject. It never links by email, creates a user, or changes memberships.
func (s Store) RebindIssuer(ctx context.Context, user uuid.UUID, subject, from, to string) error {
	if user == uuid.Nil || subject == "" || subject != strings.TrimSpace(subject) || len(subject) > 1024 || from == to || !validRebindingIssuer(from) || !validRebindingIssuer(to) {
		return ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Match Provision's identity locks and order both keys for competing repairs.
	keys := []string{from + "\n" + subject, to + "\n" + subject}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return err
		}
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active FROM identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&active); errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return ErrDenied
	} else if err != nil {
		return err
	}
	var oldUser, newUser uuid.UUID
	oldErr := tx.QueryRow(ctx, `SELECT user_id FROM identity.external_identities WHERE issuer=$1 AND subject=$2 FOR UPDATE`, from, subject).Scan(&oldUser)
	newErr := tx.QueryRow(ctx, `SELECT user_id FROM identity.external_identities WHERE issuer=$1 AND subject=$2 FOR UPDATE`, to, subject).Scan(&newUser)
	if oldErr != nil && !errors.Is(oldErr, pgx.ErrNoRows) {
		return oldErr
	}
	if newErr != nil && !errors.Is(newErr, pgx.ErrNoRows) {
		return newErr
	}
	if (oldErr == nil && oldUser != user) || (newErr == nil && newUser != user) {
		return ErrConflict
	}
	if errors.Is(oldErr, pgx.ErrNoRows) {
		if newErr == nil && newUser == user {
			return tx.Commit(ctx) // An identical completed repair is safe to retry.
		}
		return ErrDenied
	}
	if newErr == nil {
		_, err = tx.Exec(ctx, `DELETE FROM identity.external_identities WHERE issuer=$1 AND subject=$2 AND user_id=$3`, from, subject, user)
	} else {
		_, err = tx.Exec(ctx, `UPDATE identity.external_identities SET issuer=$1 WHERE issuer=$2 AND subject=$3 AND user_id=$4`, to, from, subject, user)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, user); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id FROM identity.memberships WHERE user_id=$1 ORDER BY tenant_id`, user)
	if err != nil {
		return err
	}
	var tenants []uuid.UUID
	for rows.Next() {
		var tenant uuid.UUID
		if err = rows.Scan(&tenant); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, tenant)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(tenants) == 0 {
		return ErrDenied // Every repair must have an existing tenant audit owner.
	}
	for _, tenant := range tenants {
		if err = audit(ctx, tx, tenant, "operator-cli", "external_identity.issuer_rebound", user, map[string]any{"from_issuer": from, "to_issuer": to, "subject": subject}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func validRebindingIssuer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || raw != strings.TrimSpace(raw) || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}
