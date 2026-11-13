package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/localmatching"
)

type changedProvider struct {
	fakeProvider
	calls int
}

func (p *changedProvider) Search(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
	p.calls++
	return screening.ProviderResult{}, errors.New("new provider data must not replace decision evidence")
}

func TestInlineEvidenceReplaysWithoutASecondProviderSearch(t *testing.T) {
	request := screening.SearchRequest{ForcedOutcome: "review", Provider: "opensanctions", DecisionID: "decision", ScreeningConfigID: "config", IdempotencyKey: "execution", Queries: []screening.SearchQuery{{Name: "Alex Example", Type: "Person"}}}
	raw, _ := json.Marshal(request)
	encoded, count, partial, err := localmatching.Evaluate(context.Background(), raw)
	if err != nil || count != 1 || partial {
		t.Fatal(count, partial, err)
	}
	var snapshot screening.InlineSnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		t.Fatal(err)
	}
	request.InlineResult = &snapshot
	provider := &changedProvider{}
	for i := 0; i < 2; i++ {
		result, err := screeningSearchResult(context.Background(), provider, request)
		if err != nil || len(result.Matches) != 1 || result.Matches[0].EntityID != "local-person-001" || provider.calls != 0 {
			t.Fatal(result, provider.calls, err)
		}
	}
	request.Queries[0].Name = "Morgan Sample"
	if _, err := screeningSearchResult(context.Background(), provider, request); err == nil || provider.calls != 0 {
		t.Fatal("mismatched evidence accepted or retried")
	}
	request.InlineResult = nil
	if _, err := screeningSearchResult(context.Background(), provider, request); err == nil || provider.calls != 1 {
		t.Fatal("async provider path changed")
	}
}
