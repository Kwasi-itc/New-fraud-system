package migrationaudit

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestDecisionMappingSourceAndProposalDrift(t *testing.T) {
	data, err := os.ReadFile("testdata/decision-schema-mapping.json")
	if err != nil {
		t.Fatal("generate the reviewed mapping through the disposable PG catalog test", err)
	}
	var expected Manifest
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	files, queries, err := Sources("../..")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := Build(expected.Source, files, queries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("decision schema/query/proposal mapping drift: review and regenerate using TestIntegrationDecisionSchemaManifest")
	}
	for _, table := range expected.Tables {
		if len(table.Columns) == 0 || len(table.RuntimeGrants) == 0 || len(table.Indexes) == 0 || len(table.Preflight) == 0 {
			t.Fatal("incomplete table mapping", table.Source)
		}
	}
}

func TestMappingRejectsUnknownObjectsAndTypes(t *testing.T) {
	if _, err := mapColumn("rules", Column{Name: "unknown", Type: "numeric"}); err == nil {
		t.Fatal("unknown type was silently mapped")
	}
	if _, err := mapColumn("rules", Column{Name: "unknown", Type: "text", Default: func() *string { s := "unknown_function()"; return &s }()}); err == nil {
		t.Fatal("unknown default was silently mapped")
	}
	if _, err := Build(Catalog{Tables: []Table{{Name: "core.unknown"}}}, nil, nil); err == nil {
		t.Fatal("unknown owner table was silently omitted")
	}
	if _, err := mapConstraint("rules", Constraint{Kind: "x"}); err == nil {
		t.Fatal("unknown constraint ignored")
	}
}
