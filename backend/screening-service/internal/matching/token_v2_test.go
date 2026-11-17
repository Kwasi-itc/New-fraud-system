package matching

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"testing"
)

func TestTokenProfileRetainsV1AndExplainsConservativeCaps(t *testing.T) {
	record := sourcedata.Record{Name: "Kalmora Ostiven Uverin Evaren", Type: "Person"}
	q := screening.SearchQuery{Name: "Kavmora Ostifen Uferin Evaren", Type: "Person"}
	original := Score(q, record, record.Name, "kavmora ostifen uferin evaren", "kalmora ostiven uverin evaren", .75)
	strict := ScoreV2(q, record, record.Name, "kavmora ostifen uferin evaren", "kalmora ostiven uverin evaren", .75)
	if original.Score < .85 || strict.Score != .79 || strict.NameScore != original.NameScore || strict.Attributes["nameTokenSupport"] != "multiple_changed_tokens" {
		t.Fatal(original, strict)
	}
	q.Name = "Kalmora Ostiven Uverin Evaren"
	exact := ScoreV2(q, record, record.Name, "kalmora ostiven uverin evaren", "kalmora ostiven uverin evaren", 1)
	if exact.Score != 1 || exact.Strategy != "exact_normalized" {
		t.Fatal(exact)
	}
	q.Name = "Kalmora Ostvien Uverin Evaren"
	typo := ScoreV2(q, record, record.Name, "kalmora ostvien uverin evaren", "kalmora ostiven uverin evaren", .8)
	if typo.Score < .85 || typo.Attributes["nameTokenSupport"] != "at_most_one_changed_token" {
		t.Fatal(typo)
	}
	q.BirthDate = "1980-01-01"
	record.Properties = map[string][]string{"birthDate": {"1990-01-01"}}
	if conflict := ScoreV2(q, record, record.Name, "kalmora ostvien uverin evaren", "kalmora ostiven uverin evaren", .8); conflict.Score > .49 {
		t.Fatal(conflict)
	}
}
