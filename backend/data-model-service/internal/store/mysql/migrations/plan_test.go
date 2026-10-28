package migrations

import (
	"encoding/json"
	"testing"
)

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
	want := checksum(valid.Database, s)
	for _, change := range []func(*Step){func(s *Step) { s.ID += "_new" }, func(s *Step) { s.SQL += " " }, func(s *Step) { s.After += " " }, func(s *Step) { s.Before = "different" }, func(s *Step) { s.Name += "_other" }, func(s *Step) { s.Kind = "VIEW" }} {
		changed := s
		change(&changed)
		if checksum(valid.Database, changed) == want {
			t.Fatal("checksum omitted plan input")
		}
	}
	if checksum("another_database", s) == want {
		t.Fatal("checksum omitted database")
	}
}
