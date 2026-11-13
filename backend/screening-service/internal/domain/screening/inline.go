package screening

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"
)

// InlineSnapshot is trusted owner evidence carried by the private decision
// intake. Its digest binds it to the exact query, tenant-scoped execution and
// configuration; receipt authorization remains the authority for accepting it.
type InlineSnapshot struct {
	Version       int            `json:"version"`
	RequestSHA256 string         `json:"request_sha256"`
	Result        ProviderResult `json:"result"`
}

func InlineRequestDigest(request SearchRequest) string {
	request.InlineResult = nil
	raw, _ := json.Marshal(request)
	var canonical any
	_ = json.Unmarshal(raw, &canonical)
	raw, _ = json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s InlineSnapshot) Validate(request SearchRequest) error {
	invalid := errors.New("invalid synchronous screening evidence")
	if request.ForcedOutcome != "review" && request.ForcedOutcome != "block_and_review" && request.ForcedOutcome != "decline" {
		return invalid
	}
	if s.Version != 1 || request.IsManual || request.DecisionID == "" || request.ScreeningConfigID == "" || request.IdempotencyKey == "" || s.RequestSHA256 != InlineRequestDigest(request) || len(s.Result.Matches) > 50 || !json.Valid(s.Result.RawResponse) || len(s.Result.RawResponse) > 256<<10 || s.Result.ProviderReference == "" {
		return invalid
	}
	seen := map[string]bool{}
	for _, match := range s.Result.Matches {
		if match.ExclusionOperationID != "" {
			operation, err := uuid.Parse(match.ExclusionOperationID)
			if err != nil || operation == uuid.Nil || request.UniqueCounterpartyIdentifier == nil || *request.UniqueCounterpartyIdentifier == "" {
				return invalid
			}
		}
		if match.EntityID == "" || seen[match.EntityID] || math.IsNaN(match.Score) || math.IsInf(match.Score, 0) || match.Score < 0 || match.Score > 1 || !json.Valid(match.Payload) || len(match.Payload) > 64<<10 {
			return invalid
		}
		seen[match.EntityID] = true
	}
	if s.Result.Partial && len(s.Result.Matches) == 0 {
		return invalid
	}
	return nil
}
