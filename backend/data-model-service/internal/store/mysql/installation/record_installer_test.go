package installation

import (
	"context"
	"strings"
	"testing"
)

func TestRecordInstallerPolicy(t *testing.T) {
	p := RecordInstallerPolicy{Principal: Definer{"record_installer", "%"}, Table: "tenant_fixture_records_0123456789abcdef0123456789abcdef"}
	grants, err := p.ActiveGrants("shared_app")
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 6 || grants[5] != "GRANT SET_ANY_DEFINER ON *.* TO `record_installer`@`%`" {
		t.Fatal("unexpected installer capabilities", grants)
	}
	for _, g := range grants[:5] {
		if strings.Contains(g, ".*") || strings.Contains(g, "EXECUTE") || strings.Contains(g, "GRANT OPTION") || strings.Contains(g, "DELETE") {
			t.Fatal("installer excess grant", g)
		}
	}
	a, err := p.Account(true)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := GrantStatements("shared_app", a)
	if err != nil || len(plain) != 5 || !a.Locked {
		t.Fatal("inactive policy", err)
	}
	a.Objects[0].Privileges[0] = "DELETE"
	b, err := p.Account(true)
	if err != nil || b.Objects[0].Privileges[0] != "SELECT" {
		t.Fatal("policy alias", err)
	}
	native := p
	native.NativeChanges = true
	nativeGrants, err := native.ActiveGrants("shared_app")
	if err != nil || len(nativeGrants) != 5 {
		t.Fatal("native installer", err, nativeGrants)
	}
	for _, g := range nativeGrants {
		if strings.Contains(g, "*.*") || strings.Contains(g, "DEFINER") || strings.Contains(g, "EXECUTE") {
			t.Fatal("native installer acquired global/routine capability", g)
		}
	}
	if !strings.Contains(nativeGrants[0], "ALTER") || !strings.Contains(nativeGrants[0], "TRIGGER") {
		t.Fatal("native installer cannot inspect/alter target", nativeGrants)
	}
	for _, invalid := range []RecordInstallerPolicy{{}, {Principal: Definer{"root", "%"}, Table: p.Table}, {Principal: p.Principal, Table: "data_model_exact_keys"}, {Principal: p.Principal, Table: "tenant_bad;DROP"}} {
		if _, err := invalid.ActiveGrants("shared_app"); err == nil {
			t.Fatal("unsafe policy accepted")
		}
	}
	if err := p.VerifyActive(context.Background(), nil, "shared_app"); err == nil {
		t.Fatal("nil inspector accepted")
	}
	if err := p.VerifyInactive(context.Background(), nil, "shared_app"); err == nil {
		t.Fatal("nil inactive inspector accepted")
	}
	if err := VerifyRecordInstallationSettings(context.Background(), nil); err == nil {
		t.Fatal("nil settings inspector accepted")
	}
}
