package mysql

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
)

func TestPhysicalAccountSeparation(t *testing.T) {
	makePolicies := func() []PhysicalAccessPolicy {
		return []PhysicalAccessPolicy{
			{Mode: "writer", Account: installation.AccountPolicy{User: "record_writer", Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: "tenant_records", Privileges: []string{"SELECT", "INSERT", "UPDATE"}}}}},
			{Mode: "ddl", Account: installation.AccountPolicy{User: "schema_executor", Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: "tenant_records", Privileges: []string{"SELECT", "CREATE", "ALTER", "INDEX"}}}}},
			{Mode: "reader", Account: installation.AccountPolicy{User: "record_reader", Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: "tenant_records", Privileges: []string{"SELECT"}}}}},
		}
	}
	if err := validatePhysicalPolicies("shared_app", "tenant_records", makePolicies()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func([]PhysicalAccessPolicy){
		func(p []PhysicalAccessPolicy) { p[0].Account.Locked = true },
		func(p []PhysicalAccessPolicy) { p[0].Account.User = p[1].Account.User },
		func(p []PhysicalAccessPolicy) {
			p[0].Account.Objects[0].Privileges = append(p[0].Account.Objects[0].Privileges, "ALTER")
		},
		func(p []PhysicalAccessPolicy) {
			p[1].Account.Objects[0].Privileges = append(p[1].Account.Objects[0].Privileges, "UPDATE")
		},
		func(p []PhysicalAccessPolicy) { p[0].Account.Objects[0].Name = "other_tenant" },
		func(p []PhysicalAccessPolicy) { p[0].Mode = "reader" },
		func(p []PhysicalAccessPolicy) { p[1].Mode = "unknown" },
		func(p []PhysicalAccessPolicy) {
			p[0].Account.Objects = append(p[0].Account.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "metadata", Privileges: []string{"ALTER"}})
		},
		func(p []PhysicalAccessPolicy) {
			p[0].Account.Objects = append(p[0].Account.Objects, installation.ObjectGrant{Kind: "PROCEDURE", Name: "definer_mutation", Privileges: []string{"EXECUTE"}})
		},
		func(p []PhysicalAccessPolicy) {
			p[1].Account.Objects = append(p[1].Account.Objects, installation.ObjectGrant{Kind: "FUNCTION", Name: "definer_mutation", Privileges: []string{"EXECUTE"}})
		},
		func(p []PhysicalAccessPolicy) {
			p[1].Account.Objects[0].Columns = map[string][]string{"UPDATE": {"id"}}
		},
		func(p []PhysicalAccessPolicy) {
			p[1].Account.Objects[0].Columns = map[string][]string{"INSERT": {"id"}}
		},
		func(p []PhysicalAccessPolicy) {
			p[0].Account.Objects = append(p[0].Account.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "metadata", Columns: map[string][]string{"REFERENCES": {"id"}}})
		},
		func(p []PhysicalAccessPolicy) {
			p[2].Account.Objects[0].Columns = map[string][]string{"UPDATE": {"id"}}
		},
		func(p []PhysicalAccessPolicy) {
			p[2].Account.Objects = append(p[2].Account.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "metadata", Columns: map[string][]string{"INSERT": {"id"}}})
		},
		func(p []PhysicalAccessPolicy) {
			p[0].Account.Objects[0].Privileges = []string{"SELECT", "INSERT"}
			p[0].Account.Objects[0].Columns = map[string][]string{"UPDATE": {"id"}}
		},
	} {
		policies := makePolicies()
		change(policies)
		if err := validatePhysicalPolicies("shared_app", "tenant_records", policies); err == nil {
			t.Fatal("unsafe physical role policy accepted")
		}
	}
	// A writer may have reviewed identity-column locking rights on metadata;
	// these are not permissions on the physical target or reader capabilities.
	policies := makePolicies()
	policies[0].Account.Objects = append(policies[0].Account.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "metadata", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"id"}}})
	if err := validatePhysicalPolicies("shared_app", "tenant_records", policies); err != nil {
		t.Fatal(err)
	}
}
