// Package provideraccess carries authority established by an application boundary.
// None of its values can be decoded from a customer request.
package provideraccess

import (
	"context"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
)

var ErrScope = errors.New("provider_scope_required")

type Scope struct {
	client, credential, clientRevision uuid.UUID
	principal                          identity.Principal
	operator                           bool
	scenario                           uuid.UUID
	monitor                            uuid.UUID
	inline                             uuid.UUID
	continuous                         uuid.UUID
	tenant                             uuid.UUID
	datasets                           map[string]bool
}

// FromPrincipal is called only after session and current membership verification.
func FromPrincipal(p identity.Principal) (Scope, error) {
	if p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.Subject == "" || p.AuthzVersion < 1 {
		return Scope{}, ErrScope
	}
	return Scope{principal: p}, nil
}

// GlobalOperator is for the existing authenticated private owner boundary, never
// for an absent tenant. It grants no access to tenant-private source records.
func GlobalOperator() Scope { return Scope{operator: true} }
func (s Scope) Valid() bool {
	if s.client != uuid.Nil {
		return s.credential != uuid.Nil && s.clientRevision != uuid.Nil && s.tenant != uuid.Nil
	}
	return s.operator || ((s.scenario != uuid.Nil || s.monitor != uuid.Nil || s.inline != uuid.Nil || s.continuous != uuid.Nil) && s.tenant != uuid.Nil && len(s.datasets) > 0) || (s.principal.TenantID != uuid.Nil && s.principal.SessionID != uuid.Nil && s.principal.Subject != "")
}
func (s Scope) Operator() bool { return s.operator }
func (s Scope) Tenant() uuid.UUID {
	if s.client != uuid.Nil {
		return s.tenant
	}
	if s.scenario != uuid.Nil || s.monitor != uuid.Nil || s.inline != uuid.Nil || s.continuous != uuid.Nil {
		return s.tenant
	}
	return s.principal.TenantID
}

// ScenarioExecution is minted only after the registry has checked the trusted
// producer boundary and exact persisted execution/parent/request relationship.
func ScenarioExecution(tenant, execution uuid.UUID, permittedDatasets []string) Scope {
	refs := map[string]bool{}
	for _, ref := range permittedDatasets {
		refs[ref] = true
	}
	return Scope{tenant: tenant, scenario: execution, datasets: refs}
}
func (s Scope) AllowsDataset(scope, id string) bool {
	if s.client != uuid.Nil {
		return s.datasets[scope+":"+id]
	}
	if s.scenario == uuid.Nil && s.monitor == uuid.Nil && s.inline == uuid.Nil && s.continuous == uuid.Nil {
		return true
	}
	return s.datasets[scope+":"+id]
}
func (s Scope) Scenario() uuid.UUID { return s.scenario }
func (s Scope) Monitor() uuid.UUID  { return s.monitor }

// InlineConfig is minted only by the screening-owned read facade after checking
// the live, tenant-bound canonical config. It is never decoded from HTTP JSON.
func InlineConfig(tenant, config uuid.UUID, permitted []string) Scope {
	refs := map[string]bool{}
	for _, r := range permitted {
		refs[r] = true
	}
	return Scope{tenant: tenant, inline: config, datasets: refs}
}
func (s Scope) Inline() uuid.UUID { return s.inline }
func ContinuousObject(tenant, object uuid.UUID, permitted []string) Scope {
	refs := map[string]bool{}
	for _, r := range permitted {
		refs[r] = true
	}
	return Scope{tenant: tenant, continuous: object, datasets: refs}
}
func (s Scope) Continuous() uuid.UUID { return s.continuous }

// MonitorExecution is minted only after validating the stored target, origin
// request, active subscription/current membership and its explicit dataset set.
func MonitorExecution(tenant, target uuid.UUID, permitted []string) Scope {
	refs := map[string]bool{}
	for _, ref := range permitted {
		refs[ref] = true
	}
	return Scope{tenant: tenant, monitor: target, datasets: refs}
}
func (s Scope) Principal() identity.Principal { return s.principal }
func (s Scope) Key() string {
	if s.client != uuid.Nil {
		return "client:" + s.client.String() + ":" + s.credential.String() + ":" + s.clientRevision.String()
	}
	if s.operator {
		return "global-operator"
	}
	if s.scenario != uuid.Nil {
		return s.tenant.String() + ":execution:" + s.scenario.String()
	}
	if s.monitor != uuid.Nil {
		return s.tenant.String() + ":source-target:" + s.monitor.String()
	}
	if s.inline != uuid.Nil {
		return s.tenant.String() + ":inline:" + s.inline.String()
	}
	if s.continuous != uuid.Nil {
		return s.tenant.String() + ":continuous:" + s.continuous.String()
	}
	return s.principal.TenantID.String() + ":" + s.principal.Subject + ":" + s.principal.SessionID.String()
}

type intakeKey struct{}

// MachineClient is minted only by the provider's credential verifier. Dataset
// refs and namespace come from provider-owned DB records, never request JSON.
func MachineClient(client, credential, namespace, revision uuid.UUID, permitted []string) (Scope, error) {
	if client == uuid.Nil || credential == uuid.Nil || namespace == uuid.Nil || revision == uuid.Nil {
		return Scope{}, ErrScope
	}
	refs := map[string]bool{}
	for _, ref := range permitted {
		refs[ref] = true
	}
	return Scope{client: client, credential: credential, tenant: namespace, clientRevision: revision, datasets: refs}, nil
}
func (s Scope) Client() uuid.UUID         { return s.client }
func (s Scope) Credential() uuid.UUID     { return s.credential }
func (s Scope) ClientRevision() uuid.UUID { return s.clientRevision }

func WithScenarioIntake(ctx context.Context) context.Context {
	return context.WithValue(ctx, intakeKey{}, true)
}
func IsScenarioIntake(ctx context.Context) bool { v, _ := ctx.Value(intakeKey{}).(bool); return v }

type scopeKey struct{}

func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}
func FromContext(ctx context.Context) (Scope, error) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	if !ok || !s.Valid() {
		return Scope{}, ErrScope
	}
	return s, nil
}
