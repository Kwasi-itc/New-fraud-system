package auth

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

type Capability struct {
	Name     string `json:"name"`
	Human    bool   `json:"human"`
	Service  bool   `json:"service"`
	Client   string `json:"client"` // "", "normal", or "sensitive"
	Reserved bool   `json:"reserved,omitempty"`
}

//go:embed capabilities.json
var capabilityJSON []byte
var catalog = func() map[string]Capability {
	var values []Capability
	if err := json.Unmarshal(capabilityJSON, &values); err != nil {
		panic(err)
	}
	out := make(map[string]Capability, len(values))
	for _, v := range values {
		if v.Name == "" || v.Name == "*" {
			panic("invalid capability catalog")
		}
		if _, ok := out[v.Name]; ok {
			panic("duplicate capability")
		}
		out[v.Name] = v
	}
	return out
}()

func Catalog() []Capability {
	out := make([]Capability, 0, len(catalog))
	for _, v := range catalog {
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Capability) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out
}
func LookupCapability(name string) (Capability, bool) { c, ok := catalog[name]; return c, ok }
func ValidateCapabilities(names []string, kind PrincipalType, customRole bool) error {
	if len(names) == 0 || len(names) > len(catalog) {
		return fmt.Errorf("capabilities are required and must be bounded")
	}
	seen := map[string]bool{}
	for _, name := range names {
		c, ok := catalog[name]
		if !ok || seen[name] || (customRole && c.Reserved) {
			return fmt.Errorf("unknown, duplicate, or reserved capability: %s", name)
		}
		if kind == PrincipalUser && !c.Human || kind == PrincipalClient && c.Client == "" || kind == PrincipalService && !c.Service || !validPrincipalType(kind) {
			return fmt.Errorf("capability is not permitted for principal type: %s", name)
		}
		seen[name] = true
	}
	return nil
}

// PredefinedRoles supplies explicit bundles, with no inheritance and no wildcard.
// Catalog upgrades materialize the full human bundle for Tenant Administrators.
func PredefinedRoles() map[string][]string {
	roles := map[string][]string{
		"tenant_admin":       {},
		"case_manager_admin": {"services:read", "cases:read", "cases:write", "cases:review", "cases:assign", "cases:access-all", "inboxes:read", "inboxes:manage", "reports:read", "reports:write", "reports:export", "screening:search"},
		"case_investigator":  {"services:read", "cases:read", "cases:write", "cases:review", "cases:assign", "inboxes:read", "reports:read", "reports:write", "reports:export", "screening:search"},
		"platform_viewer":    {"services:read", "models:read", "records:read", "scenarios:read", "decisions:read", "workflows:read", "screening:read", "lists:read", "executions:read"},
		"platform_builder":   {"services:read", "models:read", "models:write", "records:read", "ingest:read", "ingest:write", "scenarios:read", "scenarios:write", "decisions:read", "decisions:evaluate", "workflows:read", "workflows:write", "screening:read", "screening:write", "lists:read", "lists:write", "executions:read"},
		"platform_publisher": {"services:read", "models:read", "models:write", "models:publish", "records:read", "ingest:read", "ingest:write", "scenarios:read", "scenarios:write", "scenarios:publish", "decisions:read", "decisions:evaluate", "workflows:read", "workflows:write", "screening:read", "screening:write", "lists:read", "lists:write", "executions:read"},
		"platform_admin":     {"services:read", "models:read", "models:write", "models:publish", "records:read", "records:export", "ingest:read", "ingest:write", "scenarios:read", "scenarios:write", "scenarios:publish", "decisions:read", "decisions:evaluate", "workflows:read", "workflows:write", "screening:search", "screening:read", "screening:write", "screening:admin", "lists:read", "lists:write", "sources:read", "sources:manage", "executions:read", "executions:manage", "metrics:read", "clients:approve-sensitive"},
	}
	roles["platform_admin"] = append(roles["platform_admin"], "clients:read")
	for _, name := range []string{"platform_builder", "platform_publisher", "platform_admin"} {
		roles[name] = append(roles[name], "models:index-write")
	}
	for name := range roles {
		if name != "tenant_admin" {
			roles[name] = append(roles[name], "source-notices:read")
		}
	}
	for _, c := range Catalog() {
		if c.Human {
			roles["tenant_admin"] = append(roles["tenant_admin"], c.Name)
		}
	}
	return roles
}

// RequiresCurrentAuthority identifies protected mutations. High-throughput
// ingestion/evaluation and ordinary reads retain stateless token verification.
func RequiresCurrentAuthority(capability string) bool {
	switch capability {
	case "roles:manage", "roles:assign", "clients:manage", "clients:approve-sensitive", "members:manage", "invitations:manage", "sessions:manage", "models:write", "models:publish", "models:index-write", "scenarios:write", "scenarios:publish", "workflows:write", "screening:write", "screening:review", "screening:admin", "sources:manage", "executions:manage", "lists:write", "private-lists:import", "private-lists:approve", "inboxes:manage", "cases:write", "cases:review", "cases:assign", "reports:write", "reports:export":
		return true
	default:
		return false
	}
}
