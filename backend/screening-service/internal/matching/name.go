// Package matching scores bounded retrieved candidates. It never resolves or
// merges source identities, and its scores are not probabilities of identity.
package matching

import (
	"math"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
)

type Evidence struct {
	Field               string            `json:"field"`
	MatchedValue        string            `json:"matched_value"`
	QueryValue          string            `json:"query_value"`
	NormalizedQuery     string            `json:"normalized_query"`
	NormalizedCandidate string            `json:"normalized_candidate"`
	Strategy            string            `json:"strategy"`
	NameScore           float64           `json:"name_score"`
	Score               float64           `json:"score"`
	Attributes          map[string]string `json:"attributes"`
}

// Score receives keys made by the same versioned PostgreSQL projection for
// queries and indexed names. Source spelling and original query remain evidence.
func Score(q screening.SearchQuery, r sourcedata.Record, alias, queryKey, nameKey string, trigram float64) Evidence {
	e := Evidence{Field: "name", MatchedValue: alias, QueryValue: q.Name, NormalizedQuery: queryKey, NormalizedCandidate: nameKey, Strategy: "character_similarity", Attributes: map[string]string{}}
	if alias != r.Name {
		e.Field = "alias"
	}
	switch {
	case sourcedata.Normalize(q.Name) == sourcedata.Normalize(alias):
		e.NameScore = 1
		e.Strategy = "exact_normalized"
	case queryKey == nameKey:
		e.NameScore = .98
		e.Strategy = "diacritic_or_punctuation_fold"
	case sortedTokens(queryKey) == sortedTokens(nameKey):
		e.NameScore = .97
		e.Strategy = "word_order"
	default:
		a, b := []rune(queryKey), []rune(nameKey)
		if len(a) == 0 || len(b) == 0 {
			return e
		}
		edit := 1 - float64(distance(a, b))/float64(max(len(a), len(b)))
		e.NameScore = math.Max(0, trigram*.95)
		if edit*.96 > e.NameScore {
			e.NameScore = edit * .96
			e.Strategy = "edit_similarity"
		}
		// A single common token never qualifies as a partial multi-part identity.
		qa, nb := strings.Fields(queryKey), strings.Fields(nameKey)
		if len(qa) >= 2 && len(nb) >= 3 && len(qa) < len(nb) {
			present := map[string]bool{}
			for _, v := range nb {
				present[v] = true
			}
			complete := true
			for _, v := range qa {
				if !present[v] {
					complete = false
				}
			}
			coverage := float64(len(qa)) / float64(len(nb))
			if complete && coverage >= .66 {
				partial := .82 + .10*coverage
				if partial > e.NameScore {
					e.NameScore = partial
					e.Strategy = "partial_tokens"
				}
			}
		}
	}
	e.Score = e.NameScore
	for _, f := range []struct {
		key, value string
		strong     bool
	}{{"registrationNumber", q.RegistrationNumber, true}, {"birthDate", q.BirthDate, true}, {"country", q.Country, false}, {"nationality", q.Nationality, false}, {"address", q.Address, false}} {
		if f.value == "" {
			continue
		}
		values := r.Properties[f.key]
		if len(values) == 0 {
			e.Attributes[f.key] = "not_available"
			continue
		}
		agree := false
		for _, value := range values {
			left, right := sourcedata.Normalize(f.value), sourcedata.Normalize(value)
			if left == right || (f.key == "birthDate" && ((len(left) == 4 && strings.HasPrefix(right, left+"-")) || (len(right) == 4 && strings.HasPrefix(left, right+"-")))) {
				agree = true
			}
		}
		if agree {
			e.Attributes[f.key] = "agreement"
		} else {
			e.Attributes[f.key] = "disagreement"
			if f.strong {
				e.Score = math.Min(e.Score, .49)
			}
		}
	}
	e.NameScore = math.Round(e.NameScore*10000) / 10000
	e.Score = math.Round(e.Score*10000) / 10000
	return e
}
func sortedTokens(s string) string {
	v := strings.Fields(s)
	sort.Strings(v)
	return strings.Join(v, " ")
}

// Optimal-string-alignment distance includes adjacent transpositions. Name
// lengths are bounded by the provider; two rows keep memory bounded.
func distance(a, b []rune) int {
	previous := make([]int, len(b)+1)
	older := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				current[j] = min(current[j], older[j-2]+1)
			}
		}
		older, previous = previous, current
	}
	return previous[len(b)]
}
