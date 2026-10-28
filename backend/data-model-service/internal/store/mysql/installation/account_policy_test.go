package installation

import (
	"fmt"
	"strings"
	"testing"
)

func TestExplicitAccountPolicy(t *testing.T) {
	policy := AccountPolicy{User: "record_writer", Host: "%", Objects: []ObjectGrant{{Kind: "TABLE", Name: "tenant_records", Privileges: []string{"UPDATE", "SELECT", "INSERT"}}}}
	statements, err := GrantStatements("shared_app", policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 || !strings.Contains(statements[0], "GRANT SELECT, INSERT, UPDATE ON `shared_app`.`tenant_records`") {
		t.Fatal(statements)
	}
	for _, object := range []ObjectGrant{
		{Kind: "TABLE", Name: "*", Privileges: []string{"SELECT"}},
		{Kind: "DATABASE", Name: "tenant_records", Privileges: []string{"SELECT"}},
		{Kind: "TABLE", Name: "tenant_records", Privileges: []string{"ALL PRIVILEGES"}},
		{Kind: "TABLE", Name: "tenant_records", Privileges: []string{"SELECT", "SELECT"}},
		{Kind: "FUNCTION", Name: "resolve", Privileges: []string{"UPDATE"}},
	} {
		bad := policy
		bad.Objects = []ObjectGrant{object}
		if _, err := GrantStatements("shared_app", bad); err == nil {
			t.Fatal("invalid grant accepted")
		}
	}
}

func TestColumnPolicyBounds(t *testing.T) {
	policy := AccountPolicy{User: "operator", Host: "%", Objects: []ObjectGrant{{Kind: "TABLE", Name: "records", Columns: map[string][]string{"UPDATE": make([]string, 4097)}}}}
	if _, err := GrantStatements("shared_app", policy); err == nil {
		t.Fatal("oversized columns accepted")
	}
	columns := make([]string, 1024)
	for i := range columns {
		columns[i] = fmt.Sprintf("field_%04d_%s", i, strings.Repeat("x", 48))
	}
	policy.Objects = nil
	for i := 0; i < 20; i++ {
		policy.Objects = append(policy.Objects, ObjectGrant{Kind: "TABLE", Name: fmt.Sprintf("records_%d", i), Columns: map[string][]string{"SELECT": columns}})
	}
	if _, err := GrantStatements("shared_app", policy); err == nil {
		t.Fatal("oversized policy accepted")
	}
}

func TestColumnAccountPolicy(t *testing.T) {
	object := ObjectGrant{Kind: "TABLE", Name: "data_model_tables", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"tenant_id", "id"}}}
	policy := AccountPolicy{User: "operator", Host: "%", Objects: []ObjectGrant{object}}
	grants, err := GrantStatements("shared_app", policy)
	if err != nil || len(grants) != 1 || grants[0] != "GRANT SELECT, UPDATE (`id`, `tenant_id`) ON `shared_app`.`data_model_tables` TO `operator`@`%`" {
		t.Fatalf("grants %v %v", grants, err)
	}
	if object.Columns["UPDATE"][0] != "tenant_id" {
		t.Fatal("mutated reviewed input")
	}
	for name, bad := range map[string]ObjectGrant{
		"overlap":   {Kind: "TABLE", Name: "records", Privileges: []string{"UPDATE"}, Columns: map[string][]string{"UPDATE": {"id"}}},
		"delete":    {Kind: "TABLE", Name: "records", Columns: map[string][]string{"DELETE": {"id"}}},
		"empty":     {Kind: "TABLE", Name: "records", Columns: map[string][]string{"UPDATE": {}}},
		"duplicate": {Kind: "TABLE", Name: "records", Columns: map[string][]string{"UPDATE": {"id", "id"}}},
		"injection": {Kind: "TABLE", Name: "records", Columns: map[string][]string{"UPDATE": {"id) ON *.*"}}},
		"routine":   {Kind: "PROCEDURE", Name: "resolve", Columns: map[string][]string{"SELECT": {"id"}}},
		"wildcard":  {Kind: "TABLE", Name: "records", Columns: map[string][]string{"SELECT": {"*"}}},
	} {
		t.Run(name, func(t *testing.T) {
			p := policy
			p.Objects = []ObjectGrant{bad}
			if _, e := GrantStatements("shared_app", p); e == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	unordered := "GRANT SELECT, UPDATE (`tenant_id`, `id`) ON `shared_app`.`data_model_tables` TO `operator`@`%`"
	if canonicalGrantColumns(unordered) != grants[0] {
		t.Fatal("column order not canonical")
	}
	for _, extra := range []string{strings.Replace(unordered, "`id`)", "`id`, `other`)", 1), unordered + " WITH GRANT OPTION", strings.Replace(unordered, "UPDATE (`tenant_id`, `id`)", "UPDATE", 1)} {
		if canonicalGrantColumns(extra) == grants[0] {
			t.Fatal("broader grant normalized away")
		}
	}
}
