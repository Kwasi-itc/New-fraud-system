package matching

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"testing"
)

func TestExplainableNameScoring(t *testing.T) {
	r := sourcedata.Record{Name: "Ama Elin Testperson", Type: "Person", Properties: map[string][]string{"birthDate": {"1985-04-03"}}}
	for _, tc := range []struct {
		name, query, alias, key, other, strategy string
		min                                      float64
	}{
		{"exact", "Ama Elin Testperson", r.Name, "ama elin testperson", "ama elin testperson", "exact_normalized", 1},
		{"reordered", "Testperson Ama Elin", r.Name, "testperson ama elin", "ama elin testperson", "word_order", .97},
		{"partial", "Ama Testperson", r.Name, "ama testperson", "ama elin testperson", "partial_tokens", .88},
		{"typo", "Ama Elin Testpreson", r.Name, "ama elin testpreson", "ama elin testperson", "edit_similarity", .90},
		{"alias", "Ama Example", "Ama Example", "ama example", "ama example", "exact_normalized", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Score(screening.SearchQuery{Name: tc.query, Type: "Person"}, r, tc.alias, tc.key, tc.other, .1)
			if e.Score < tc.min || e.Strategy != tc.strategy || e.MatchedValue != tc.alias {
				t.Fatal(e)
			}
		})
	}
	q := screening.SearchQuery{Name: r.Name, Type: "Person", BirthDate: "1999-01-01"}
	e := Score(q, r, r.Name, "ama elin testperson", "ama elin testperson", 1)
	if e.Score >= .5 || e.NameScore != 1 || e.Attributes["birthDate"] != "disagreement" {
		t.Fatal("conflict concealed", e)
	}
	q.BirthDate = "1985"
	e = Score(q, r, r.Name, "ama elin testperson", "ama elin testperson", 1)
	if e.Score != 1 || e.Attributes["birthDate"] != "agreement" {
		t.Fatal("date precision", e)
	}
}
