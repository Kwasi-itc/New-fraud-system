package matching

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"testing"
)

func TestIdentifierEvidenceDoesNotResolveIdentityOrEraseConflicts(t *testing.T) {
	id := sourcedata.Identifier{Type: "lei", Value: "001-Ab"}
	r := sourcedata.Record{Name: "Fictional Company", Type: "Organization", Identifiers: []sourcedata.Identifier{id}}
	q := screening.SearchQuery{Name: r.Name, Type: r.Type, Identifiers: []sourcedata.Identifier{id}}
	if e := ScoreIdentifiers(q, r, r.Name, "fictional company", "fictional company", 1); e.Score != 1 || e.Strategy != "exact_identifier_with_name_evidence" {
		t.Fatal(e)
	}
	q.Identifiers[0].Value = "different"
	if e := ScoreIdentifiers(q, r, r.Name, "fictional company", "fictional company", 1); e.Score != .49 || e.Attributes["identifier_1_lei"] != "different_value" {
		t.Fatal(e)
	}
	q.Identifiers[0].Type = "passport"
	q.Identifiers[0].Country = "gh"
	r.Identifiers = []sourcedata.Identifier{{Type: "passport", Value: "another", Country: "gh"}}
	if e := ScoreIdentifiers(q, r, r.Name, "fictional company", "fictional company", 1); e.Score != 1 {
		t.Fatal("different passport erased name evidence", e)
	}
	q.BirthDate = "1990"
	r.Properties = map[string][]string{"birthDate": {"1980"}}
	q.Identifiers[0].Value = "another"
	if e := ScoreIdentifiers(q, r, r.Name, "fictional company", "fictional company", 1); e.Score != .49 {
		t.Fatal("ID erased date conflict", e)
	}
}
