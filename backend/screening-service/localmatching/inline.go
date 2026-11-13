package localmatching

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

// Evaluate performs a synthetic search without database writes or network
// calls. The returned snapshot is replayed by the owner, never searched again.
func Evaluate(ctx context.Context, raw json.RawMessage) (json.RawMessage, int, bool, error) {
	var request screening.SearchRequest
	if len(raw) > 64<<10 || json.Unmarshal(raw, &request) != nil || request.InlineResult != nil {
		return nil, 0, false, errors.New("invalid inline query")
	}
	result, err := (LocalProvider{}).Search(ctx, request)
	if err != nil {
		return nil, 0, false, err
	}
	snapshot := screening.InlineSnapshot{Version: 1, RequestSHA256: screening.InlineRequestDigest(request), Result: result}
	if err := snapshot.Validate(request); err != nil {
		return nil, 0, false, err
	}
	encoded, err := json.Marshal(snapshot)
	return encoded, len(result.Matches), result.Partial, err
}
