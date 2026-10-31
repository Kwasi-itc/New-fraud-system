package migrations

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestExplicitOwnerPreservesReviewedPlan(t *testing.T) {
	definition, _ := json.Marshal(map[string]string{"Table": "owned", "Create Table": "reviewed"})
	p := Plan{Database: "shared_app", Owner: "ingestion", Steps: []Step{{ID: "create_owned", Kind: "TABLE", Name: "owned", SQL: "CREATE TABLE shared_app.owned(id INT)", After: string(definition)}}}
	for _, owner := range []string{"", "data_model", strings.Repeat("a", 47)} {
		if _, err := NewForOwner(&sql.DB{}, owner, p); err == nil {
			t.Fatal("owner mismatch accepted", owner)
		}
	}
	r, err := NewForOwner(&sql.DB{}, "ingestion", p)
	if err != nil || r.plan.ledgerName() != "ingestion_schema_migrations" {
		t.Fatal(r, err)
	}
	p.Owner = ""
	legacy, err := NewForOwner(&sql.DB{}, "data_model", p)
	if err != nil || legacy.ledgerDDL() != ledgerCreate {
		t.Fatal("legacy ledger drift", err)
	}
	if _, err := NewForOwner(&sql.DB{}, strings.Repeat("a", 46), p); err != nil {
		t.Fatal(err)
	}
	if _, err := NewForOwner(&sql.DB{}, strings.Repeat("a", 47), p); err == nil {
		t.Fatal("oversized owner accepted")
	}
}

func TestPlanValidation(t *testing.T) {
	definition, _ := json.Marshal(map[string]string{"Table": "data_model_example", "Create Table": "reviewed definition"})
	s := Step{ID: "create_example", Kind: "TABLE", Name: "data_model_example", SQL: "CREATE TABLE shared_app.data_model_example(id INT)", After: string(definition)}
	valid := Plan{Database: "shared_app", Steps: []Step{s}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Step){
		func(s *Step) { s.ID = "invalid-name" }, func(s *Step) { s.Kind = "USER" }, func(s *Step) { s.Name = "data_model_schema_migrations" },
		func(s *Step) { s.SQL = "DROP TABLE shared_app.data_model_example" }, func(s *Step) { s.After = "{}" }, func(s *Step) { s.Before = s.After },
	} {
		bad := s
		change(&bad)
		if err := (Plan{Database: "shared_app", Steps: []Step{bad}}).Validate(); err == nil {
			t.Fatalf("invalid step accepted: %+v", bad)
		}
	}
	if err := (Plan{Database: "shared_app", Steps: []Step{s, s}}).Validate(); err == nil {
		t.Fatal("duplicate step accepted")
	}
	next := s
	next.ID = "upgrade_example"
	next.Before = ""
	if err := (Plan{Database: "shared_app", Steps: []Step{s, next}}).Validate(); err == nil {
		t.Fatal("broken shape chain accepted")
	}
	want := Checksum(valid.Database, s)
	for _, change := range []func(*Step){func(s *Step) { s.ID += "_new" }, func(s *Step) { s.SQL += " " }, func(s *Step) { s.After += " " }, func(s *Step) { s.Before = "different" }, func(s *Step) { s.Name += "_other" }, func(s *Step) { s.Kind = "VIEW" }} {
		changed := s
		change(&changed)
		if Checksum(valid.Database, changed) == want {
			t.Fatal("checksum omitted plan input")
		}
	}
	if Checksum("another_database", s) == want {
		t.Fatal("checksum omitted database")
	}
}
