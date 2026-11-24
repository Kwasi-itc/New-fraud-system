package platform

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Profile struct {
	UserID        uuid.UUID `json:"user_id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	TenantName    string    `json:"tenant_name"`
	Name          string    `json:"name"`
	AccountName   string    `json:"account_name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	RoleID        string    `json:"role_id"`
	RoleName      string    `json:"role_name"`
	Version       int64     `json:"version"`
}

func (s Store) Profile(ctx context.Context, raw string) (Profile, error) {
	p, err := s.principalForSession(ctx, raw)
	if err != nil {
		return Profile{}, err
	}
	return s.ProfileForPrincipal(ctx, p)
}

func (s Store) ProfileForPrincipal(ctx context.Context, p auth.Principal) (Profile, error) {
	userID, err := uuid.Parse(p.Subject)
	if !p.IsHuman() || err != nil || userID == uuid.Nil || p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.AuthzVersion < 1 {
		return Profile{}, ErrDenied
	}
	var out Profile
	err = s.DB.QueryRow(ctx, `SELECT u.id,m.tenant_id,t.name,u.display_name,
 coalesce(e.account_name,''),coalesce(e.email,''),coalesce(e.email_verified,false),r.id,r.name,u.profile_version
 FROM identity.sessions sess JOIN identity.users u ON u.id=sess.user_id
 JOIN identity.memberships m ON m.tenant_id=sess.tenant_id AND m.user_id=sess.user_id
 JOIN core.tenants t ON t.id=m.tenant_id
 JOIN identity.roles r ON r.tenant_id=m.tenant_id AND r.id=m.roles[1]
 LEFT JOIN identity.external_identities e ON e.issuer=sess.identity_issuer AND e.subject=sess.identity_subject AND e.user_id=u.id
 WHERE sess.id=$1 AND sess.user_id=$2 AND sess.tenant_id=$3 AND m.version=$4
 AND sess.revoked_at IS NULL AND sess.expires_at>now() AND sess.idle_until>now()
 AND u.active AND m.active AND t.status='active' AND cardinality(m.roles)=1`, p.SessionID, userID, p.TenantID, p.AuthzVersion).Scan(&out.UserID, &out.TenantID, &out.TenantName, &out.Name, &out.AccountName, &out.Email, &out.EmailVerified, &out.RoleID, &out.RoleName, &out.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrDenied
	}
	return out, err
}

func (s Store) ChangeProfile(ctx context.Context, raw, name string, version int64) error {
	p, err := s.principalForSession(ctx, raw)
	if err != nil {
		return err
	}
	return s.ChangeProfileForPrincipal(ctx, p, name, version)
}

func (s Store) ChangeProfileForPrincipal(ctx context.Context, p auth.Principal, name string, version int64) error {
	name = strings.TrimSpace(name)
	userID, parseErr := uuid.Parse(p.Subject)
	if !p.IsHuman() || parseErr != nil || userID == uuid.Nil || p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.AuthzVersion < 1 || version < 1 || name == "" || len(name) > 200 || strings.ContainsFunc(name, unicode.IsControl) {
		return ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var active bool
	var current int64
	var previousName string
	if err = tx.QueryRow(ctx, `SELECT active,profile_version,display_name FROM identity.users WHERE id=$1 FOR UPDATE`, userID).Scan(&active, &current, &previousName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		}
		return err
	}
	if !active {
		return ErrDenied
	}
	if current != version {
		return ErrConflict
	}
	var membershipVersion int64
	if err = tx.QueryRow(ctx, `SELECT active,version FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, p.TenantID, userID).Scan(&active, &membershipVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		}
		return err
	}
	if !active || membershipVersion != p.AuthzVersion {
		return ErrDenied
	}
	if err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() FROM identity.sessions WHERE id=$1 AND user_id=$2 AND tenant_id=$3 FOR SHARE`, p.SessionID, userID, p.TenantID).Scan(&active); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		}
		return err
	}
	if !active {
		return ErrDenied
	}
	if err = validatePrincipal(ctx, tx, p); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.users SET display_name=$2,profile_version=profile_version+1 WHERE id=$1`, userID, name); err != nil {
		return err
	}
	if err = audit(ctx, tx, p.TenantID, p.Subject, "profile.updated", userID, map[string]any{"previous_name": previousName, "name": name, "version": version + 1}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Provider metadata is display-only and never links accounts or grants access.
func captureProviderIdentity(ctx context.Context, tx pgx.Tx, id oidcprovider.Identity) error {
	if len(id.AccountName) > 200 || len(id.Email) > 254 || strings.ContainsFunc(id.AccountName, unicode.IsControl) || strings.ContainsFunc(id.Email, unicode.IsControl) {
		return ErrDenied
	}
	tag, err := tx.Exec(ctx, `UPDATE identity.external_identities SET account_name=$3,email=$4,email_verified=$5 WHERE issuer=$1 AND subject=$2`, id.Issuer, id.Subject, id.AccountName, id.Email, id.EmailVerified)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDenied
	}
	return nil
}

func (s Server) profile(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	out, err := s.Store.ProfileForPrincipal(r.Context(), p)
	if err != nil {
		s.authorizationError(w, err)
		return
	}
	respond(w, out)
}
func (s Server) changeProfile(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Store.ChangeProfileForPrincipal(r.Context(), p, in.Name, in.Version); err != nil {
		s.authorizationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
