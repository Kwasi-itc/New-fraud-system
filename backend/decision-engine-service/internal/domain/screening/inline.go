package screening

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrInlineUnavailable = errors.New("synchronous screening is unavailable")
var ErrInlineFailed = errors.New("synchronous screening could not complete")

// Evaluation returns the owner's detached match evidence. It is persisted with
// the decision's effect and replayed by the screening owner after commit.
type InlineEvaluation struct {
	Result     json.RawMessage
	MatchCount int
	Partial    bool
}
type InlineEvaluator func(context.Context, string, json.RawMessage) (InlineEvaluation, error)
