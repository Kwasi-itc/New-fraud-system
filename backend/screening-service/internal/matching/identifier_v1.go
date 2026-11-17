package matching

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"math"
	"strconv"
)

// ScoreIdentifiers is a separate, uncalibrated evidence profile. Frozen name
// profiles remain unchanged. An exact identifier creates a review candidate,
// never a legal identity assertion or an entity merge. Missing/differently
// scoped identifiers do not count as disagreement. Multiple valid passports
// may coexist; a different passport is not proof of a different person.
func ScoreIdentifiers(q screening.SearchQuery, r sourcedata.Record, alias, queryKey, nameKey string, trigram float64) Evidence {
	e := ScoreV2(q, r, alias, queryKey, nameKey, trigram)
	agrees, hardConflict := false, false
	for i, id := range q.Identifiers {
		state, found := "not_available", false
		for _, candidate := range r.Identifiers {
			if !sourcedata.IdentifierNamespace(id, candidate) {
				continue
			}
			if state == "not_available" {
				state = "different_value"
			}
			if sourcedata.NormalizeIdentifier(id.Value) == sourcedata.NormalizeIdentifier(candidate.Value) {
				state = "exact_agreement"
				found = true
				break
			}
		}
		e.Attributes["identifier_"+strconv.Itoa(i+1)+"_"+id.Type] = state
		agrees = agrees || found
		hardConflict = hardConflict || (!found && state == "different_value" && (id.Type == "lei" || id.Type == "imo"))
	}
	if agrees {
		e.Strategy = "exact_identifier_with_name_evidence"
		// A strong date/registration disagreement remains visible and cannot be
		// overridden by another identifier. No name threshold is needed for indexed
		// exact-ID retrieval, allowing aliases in a different script to be reviewed.
		e.Score = 1
		if e.Attributes["birthDate"] == "disagreement" || e.Attributes["registrationNumber"] == "disagreement" {
			e.Score = .49
		}
	}
	if hardConflict {
		e.Score = math.Min(e.Score, .49)
	}
	return e
}
