package investigator

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CheckRoles is the shared auth-owner read contract for an explicitly configured
// feature role mapping. Required roles come from server policy, never JWT role
// claims or customer JSON. Mutations lock user -> membership -> session, matching
// the existing case/data-model contracts and auth revocation order.
func CheckRoles(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, p Principal, required []string, lock bool) error {
	actor, err := uuid.Parse(p.Subject)
	if err != nil || actor == uuid.Nil || p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.AuthzVersion < 1 || len(required) == 0 || len(required) > 7 {
		return Forbidden()
	}
	known := map[string]bool{"tenant_admin": true, "case_manager_admin": true, "case_investigator": true, "platform_viewer": true, "platform_builder": true, "platform_publisher": true, "platform_admin": true}
	for _, role := range required {
		if !known[role] {
			return Forbidden()
		}
	}
	// The auth owner supplies row-lock authority without granting this service
	// UPDATE on accounts, roles or sessions. Legacy deployments remain readable
	// during migration; restricted writers require auth schema v4 at activation.
	var ownerContract bool
	if err = db.QueryRow(ctx, `SELECT to_regprocedure('identity.authorize_roles_v1(text,uuid,uuid,bigint,text[],boolean)') IS NOT NULL`).Scan(&ownerContract); err != nil {
		return &Error{503, "Identity verification is temporarily unavailable."}
	}
	if ownerContract {
		var allowed bool
		if err = db.QueryRow(ctx, `SELECT identity.authorize_roles_v1($1,$2,$3,$4,$5,$6)`, p.Subject, p.TenantID, p.SessionID, p.AuthzVersion, required, lock).Scan(&allowed); err != nil {
			return &Error{503, "Identity verification is temporarily unavailable."}
		}
		if !allowed {
			return Forbidden()
		}
		return nil
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	unavailable := func(err error) error {
		if errors.Is(err, pgx.ErrNoRows) {
			return Forbidden()
		}
		return &Error{503, "Identity verification is temporarily unavailable."}
	}
	var active bool
	if err = db.QueryRow(ctx, `SELECT active FROM identity.users WHERE id=$1`+suffix, actor).Scan(&active); err != nil {
		return unavailable(err)
	}
	if !active {
		return Forbidden()
	}
	var allowed, memberActive bool
	var version int64
	if err = db.QueryRow(ctx, `SELECT active,roles && $3::text[],version FROM identity.memberships WHERE user_id=$1 AND tenant_id=$2`+suffix, actor, p.TenantID, required).Scan(&memberActive, &allowed, &version); err != nil {
		return unavailable(err)
	}
	if !memberActive || !allowed || version != p.AuthzVersion {
		return Forbidden()
	}
	if err = db.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() AND EXISTS(SELECT 1 FROM core.tenants WHERE id=$2 AND status='active') FROM identity.sessions WHERE user_id=$1 AND tenant_id=$2 AND id=$3`+suffix, actor, p.TenantID, p.SessionID).Scan(&active); err != nil {
		return unavailable(err)
	}
	if !active {
		return Forbidden()
	}
	return nil
}
