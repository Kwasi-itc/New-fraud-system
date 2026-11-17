package postgres_test

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// These slices explain the existing frozen report; they never relabel queries,
// change thresholds or count incomplete output as a correct clean result.
func qualityDiagnostics(items []qualityObservation, threshold int) map[string]any {
	counts := map[string]map[string]int{}
	examples := []map[string]any{}
	tokenRecords := map[string]map[string]bool{}
	for _, o := range items {
		for _, c := range o.Candidates {
			for _, token := range strings.Fields(c.Evidence.NormalizedCandidate) {
				if tokenRecords[token] == nil {
					tokenRecords[token] = map[string]bool{}
				}
				tokenRecords[token][c.Ref.RecordID] = true
			}
		}
	}
	add := func(key, kind string) {
		if counts[key] == nil {
			counts[key] = map[string]int{}
		}
		counts[key][kind]++
	}
	for _, o := range items {
		if o.Case.Indeterminate {
			continue
		}
		want := map[string]bool{}
		for _, id := range o.Case.Relevant {
			want[id] = true
		}
		chosen := 0
		for _, c := range o.Candidates {
			if c.Evidence.Score < float64(threshold)/100 || chosen >= 50 {
				continue
			}
			chosen++
			kind := "false_positive_candidate"
			if want[c.Ref.RecordID] {
				kind = "true_positive_candidate"
			}
			length := "16_plus_runes"
			if utf8.RuneCountInString(o.Case.Query.Name) <= 5 {
				length = "1_to_5_runes"
			} else if utf8.RuneCountInString(o.Case.Query.Name) <= 15 {
				length = "6_to_15_runes"
			}
			common := false
			for _, token := range strings.Fields(c.Evidence.NormalizedQuery) {
				if len(tokenRecords[token]) >= 12 {
					common = true
				}
			}
			idState := "no_identifier_or_date_query"
			if o.Case.Query.BirthDate != "" || o.Case.Query.RegistrationNumber != "" || len(o.Case.Query.Identifiers) > 0 {
				idState = "query_present_source_missing"
				for _, v := range c.Evidence.Attributes {
					if v == "agreement" || v == "exact_agreement" {
						idState = "agreement"
					}
					if v == "disagreement" || v == "different_value" {
						idState = "conflict"
						break
					}
				}
			}
			keys := []string{"partition:" + o.Case.Partition, "query_source_label:" + o.Case.Source, "candidate_dataset:" + c.Ref.Dataset.ID, "query_category_label:" + o.Case.Category, "entity_type:" + c.Record.Type, "strategy:" + c.Evidence.Strategy, "matched_field:" + c.Evidence.Field, "name_length:" + length, fmt.Sprintf("common_token_at_least_12_records:%t", common), "identifier_context:" + idState, "stratum:" + o.Case.Stratum}
			for _, key := range keys {
				add(key, kind)
			}
			if kind == "false_positive_candidate" && o.Case.Partition == "development" && len(examples) < 30 {
				examples = append(examples, map[string]any{"query_id": o.Case.ID, "candidate_record_id": c.Ref.RecordID, "score": c.Evidence.Score, "strategy": c.Evidence.Strategy, "field": c.Evidence.Field, "identifier_context": idState})
			}
		}
	}
	return map[string]any{"threshold": threshold, "counts": counts, "development_examples": examples, "source_scope": "quality-global/private are fictional dataset labels; category labels are synthetic query strata and confounded with entity type. They are not measurements of official OFAC/UN/UK quality.", "commonality_definition": "A query token appears in at least 12 distinct retrieved corpus records; diagnostic only, never used to tune scoring.", "transliteration_scope": "Explicit synthetic native-script aliases only; automatic transliteration is not implemented or assessed."}
}
