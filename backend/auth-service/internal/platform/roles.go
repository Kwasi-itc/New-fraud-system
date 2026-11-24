package platform

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var customRoleID = regexp.MustCompile(`^role_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validRoleID(id string) bool {
	_, builtin := auth.PredefinedRoles()[id]
	return builtin || customRoleID.MatchString(id)
}
func principal(s Session) auth.Principal {
	caps := make(map[string]bool, len(s.Capabilities))
	for _, c := range s.Capabilities {
		caps[c] = true
	}
	return auth.Principal{Type: auth.PrincipalUser, Subject: s.UserID.String(), TenantID: s.TenantID, SessionID: s.ID, AuthzVersion: s.Version, Roles: s.Roles, Capabilities: caps, Admin: caps["cases:access-all"]}
}

type Role struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Builtin      bool     `json:"builtin"`
	Capabilities []string `json:"capabilities"`
	Version      int64    `json:"version"`
}
type RoleChange struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Version      int64    `json:"version"`
}

func (s Store) Role(ctx context.Context, p auth.Principal, id string) (Role, error) {
	if !p.IsHuman() || !p.Can("roles:read") || !validRoleID(id) {
		return Role{}, ErrDenied
	}
	var role Role
	err := s.DB.QueryRow(ctx, `SELECT id,name,builtin,capabilities,version FROM identity.roles WHERE tenant_id=$1 AND id=$2`, p.TenantID, id).Scan(&role.ID, &role.Name, &role.Builtin, &role.Capabilities, &role.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, &auth.Error{Code: 404, Message: "Role not found."}
	}
	return role, err
}

func (s Store) Roles(ctx context.Context, p auth.Principal, after string) ([]Role, error) {
	if !p.IsHuman() || !p.Can("roles:read") {
		return nil, ErrDenied
	}
	rows, err := s.DB.Query(ctx, `SELECT id,name,builtin,capabilities,version FROM identity.roles WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 101`, p.TenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		if err = rows.Scan(&r.ID, &r.Name, &r.Builtin, &r.Capabilities, &r.Version); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// administrativeTx serializes only identity administrative mutations in one tenant.
// Investigation transactions share identity/role read locks and do not acquire this lock.
func (s Store) administrativeTx(ctx context.Context, p auth.Principal, capability string, fn func(pgx.Tx) error) error {
	if !p.IsHuman() || p.TenantID == uuid.Nil || !p.Can(capability) {
		return ErrDenied
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if capability == "roles:assign" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "identity-admin:"+p.TenantID.String()); err != nil {
			return err
		}
	}
	if err = auth.CheckCapability(ctx, tx, p, capability, true); err != nil {
		return err
	}
	if capability == "roles:manage" || capability == "roles:assign" {
		var builtin bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.member_roles WHERE tenant_id=$1 AND user_id=$2 AND role_id='tenant_admin')`, p.TenantID, p.Subject).Scan(&builtin); err != nil {
			return err
		}
		if !builtin {
			return ErrDenied
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s Store) SaveRole(ctx context.Context, p auth.Principal, id string, in RoleChange) (Role, error) {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 1 || len(in.Name) > 100 {
		return Role{}, auth.Invalid("Role name must be 1–100 bytes.")
	}
	if err := auth.ValidateCapabilities(in.Capabilities, auth.PrincipalUser, true); err != nil {
		return Role{}, auth.Invalid(err.Error())
	}
	for key, caps := range auth.PredefinedRoles() {
		_ = caps
		if strings.EqualFold(in.Name, key) || strings.EqualFold(in.Name, strings.ReplaceAll(key, "_", " ")) || strings.EqualFold(in.Name, "Tenant Administrator") {
			return Role{}, auth.Invalid("Predefined role names are reserved.")
		}
	}
	creating := id == ""
	if creating {
		id = "role_" + uuid.NewString()
	} else if !customRoleID.MatchString(id) || in.Version < 1 {
		return Role{}, ErrDenied
	}
	caps := slices.Clone(in.Capabilities)
	slices.Sort(caps)
	out := Role{}
	err := s.administrativeTx(ctx, p, "roles:manage", func(tx pgx.Tx) error {
		var row pgx.Row
		if creating {
			row = tx.QueryRow(ctx, `INSERT INTO identity.roles(tenant_id,id,name,capabilities) VALUES($1,$2,$3,$4) RETURNING id,name,builtin,capabilities,version`, p.TenantID, id, in.Name, caps)
		} else {
			row = tx.QueryRow(ctx, `UPDATE identity.roles SET name=$3,capabilities=$4,version=version+1 WHERE tenant_id=$1 AND id=$2 AND NOT builtin AND version=$5 RETURNING id,name,builtin,capabilities,version`, p.TenantID, id, in.Name, caps, in.Version)
		}
		if err := row.Scan(&out.ID, &out.Name, &out.Builtin, &out.Capabilities, &out.Version); errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		return audit(ctx, tx, p.TenantID, p.Subject, "role.saved", uuid.MustParse(strings.TrimPrefix(id, "role_")), out)
	})
	return out, err
}
func (s Store) DeleteRole(ctx context.Context, p auth.Principal, id string, version int64) error {
	if !customRoleID.MatchString(id) || version < 1 {
		return ErrDenied
	}
	return s.administrativeTx(ctx, p, "roles:manage", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM identity.roles WHERE tenant_id=$1 AND id=$2 AND NOT builtin AND version=$3`, p.TenantID, id, version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return audit(ctx, tx, p.TenantID, p.Subject, "role.deleted", uuid.MustParse(strings.TrimPrefix(id, "role_")), map[string]any{"role_id": id, "version": version})
	})
}
