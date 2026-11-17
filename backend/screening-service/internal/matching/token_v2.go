package matching

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"math"
	"strings"
)

// ScoreV2 is a distinct conservative profile. A long full-name edit ratio can
// hide several independent token changes; cap that weak support rather than
// interpreting the long string's score as corroborating identity evidence.
// Exact/alias/fold/order meanings and the original v1 scorer remain unchanged.
func ScoreV2(q screening.SearchQuery, r sourcedata.Record, alias, queryKey, nameKey string, trigram float64) Evidence {
	e := Score(q, r, alias, queryKey, nameKey, trigram)
	if e.Strategy == "exact_normalized" || e.Strategy == "diacritic_or_punctuation_fold" || e.Strategy == "word_order" {
		return e
	}
	left, right := strings.Fields(queryKey), strings.Fields(nameKey)
	if len(left) < 2 || len(right) < 2 {
		return e
	}
	remaining := append([]string(nil), right...)
	changed := 0
	for _, token := range left {
		exact := -1
		for j, candidate := range remaining {
			if token == candidate {
				exact = j
				break
			}
		}
		if exact >= 0 {
			remaining = append(remaining[:exact], remaining[exact+1:]...)
		} else {
			changed++
		}
	}
	// For two-token aliases, more than one changed token is particularly weak.
	// A single supported typo is retained. Strong attribute conflicts still cap
	// below 0.50 in v1 and cannot be raised by this profile.
	if changed >= 2 {
		e.Score = math.Min(e.Score, .79)
		e.Attributes["nameTokenSupport"] = "multiple_changed_tokens"
		e.Strategy += "_token_capped"
	} else {
		e.Attributes["nameTokenSupport"] = "at_most_one_changed_token"
	}
	return e
}
