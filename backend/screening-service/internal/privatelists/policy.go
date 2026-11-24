package privatelists

import (
	"context"
	"errors"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/jackc/pgx/v5"
)

var ErrInput = errors.New("invalid_private_list_input")
var ErrPolicy = errors.New("private_list_permissions_unconfigured")
var ErrConflict = errors.New("private_list_conflict")
var ErrNotFound = errors.New("private_list_not_found")

type Policy struct {
	ImportCapability  string
	ApproveCapability string
}

func TenantAdministratorPolicy() Policy {
	return Policy{"private-lists:import", "private-lists:approve"}
}
func NewPolicy(importCapability, approveCapability string) (Policy, error) {
	if importCapability == "" && approveCapability == "" {
		return Policy{}, nil
	}
	if importCapability != "private-lists:import" || approveCapability != "private-lists:approve" {
		return Policy{}, ErrPolicy
	}
	return TenantAdministratorPolicy(), nil
}
func (p Policy) Configured() bool { return p.ImportCapability != "" && p.ApproveCapability != "" }
func (p Policy) check(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor identity.Principal, action string, lock bool) error {
	if !p.Configured() {
		return ErrPolicy
	}
	capability := "private-lists:read"
	if action == "import" {
		capability = p.ImportCapability
	}
	if action == "approve" {
		capability = p.ApproveCapability
	}
	if !actor.IsHuman() || !actor.Can(capability) {
		return identity.Forbidden()
	}
	if !lock {
		return nil
	}
	return identity.CheckCapability(ctx, q, actor, capability, true)
}
