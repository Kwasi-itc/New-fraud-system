package main

import "testing"

func TestFixtureExport(t *testing.T) {
	m, e := build("ingestion_full_test_0123456789abcdef0123456789abcdef", "0123456789abcdef")
	if e != nil || len(m.Steps) < 70 || len(m.Create) != 5 || len(m.Drop) != 5 || len(m.Grants) == 0 {
		t.Fatal(m, e)
	}
	if m.Steps[len(m.Steps)-1].ID != "data_model_v006_contract_lookup" {
		t.Fatal("missing contract upgrade")
	}
	for _, db := range []string{"mysql", "application", "ingestion_full_test_existing", "ingestion_full_test_0123456789abcdef0123456789abcde`"} {
		if _, e := build(db, "0123456789abcdef"); e == nil {
			t.Fatal("unsafe database", db)
		}
	}
	if _, e := build("ingestion_full_test_0123456789abcdef0123456789abcdef", "root"); e == nil {
		t.Fatal("unsafe account")
	}
}
