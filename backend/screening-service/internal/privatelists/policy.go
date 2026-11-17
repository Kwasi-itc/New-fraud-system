package privatelists

import (
	"context"
	"errors"
	"strings"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/jackc/pgx/v5"
)

var ErrInput = errors.New("invalid_private_list_input")
var ErrPolicy = errors.New("private_list_permissions_unconfigured")
var ErrConflict = errors.New("private_list_conflict")
var ErrNotFound = errors.New("private_list_not_found")

type Policy struct {
	ImportRoles  []string
	ApproveRoles []string
}

// Approved by the user: both operations belong to tenant administrators, with
// a different authenticated person required by the publication transaction.
func TenantAdministratorPolicy() Policy {
	return Policy{ImportRoles: []string{"tenant_admin"}, ApproveRoles: []string{"tenant_admin"}}
}

func NewPolicy(importRoles, approveRoles string) (Policy, error) {
	parse := func(raw string) ([]string, error) {
		out := []string{}
		seen := map[string]bool{}
		known := map[string]bool{"tenant_admin": true, "case_manager_admin": true, "case_investigator": true, "platform_viewer": true, "platform_builder": true, "platform_publisher": true, "platform_admin": true}
		if raw == "" {
			return out, nil
		}
		for _, role := range strings.Split(raw, ",") {
			role = strings.TrimSpace(role)
			if !known[role] || seen[role] {
				return nil, ErrPolicy
			}
			seen[role] = true
			out = append(out, role)
		}
		return out, nil
	}
	in, e := parse(importRoles)
	if e != nil {
		return Policy{}, e
	}
	approve, e := parse(approveRoles)
	if e != nil {
		return Policy{}, e
	}
	if (len(in) == 0) != (len(approve) == 0) {
		return Policy{}, ErrPolicy
	}
	return Policy{in, approve}, nil
}
func (p Policy) Configured() bool { return len(p.ImportRoles) > 0 && len(p.ApproveRoles) > 0 }
func (p Policy) readRoles() []string {
	out := []string{}
	seen := map[string]bool{}
	for _, role := range append(append([]string{}, p.ImportRoles...), p.ApproveRoles...) {
		if !seen[role] {
			seen[role] = true
			out = append(out, role)
		}
	}
	return out
}
func (p Policy) check(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor identity.Principal, action string, lock bool) error {
	if !p.Configured() {
		return ErrPolicy
	}
	roles := p.readRoles()
	if action == "import" {
		roles = p.ImportRoles
	}
	if action == "approve" {
		roles = p.ApproveRoles
	}
	return identity.CheckRoles(ctx, q, actor, roles, lock)
}
