package mysqlruntime

import (
	"context"
	"encoding/json"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"strings"
	"testing"
)

func TestLocalProviderMatchingBoundariesAndScope(t *testing.T) {
	p := LocalProvider{}
	for _, test := range []struct {
		name, kind      string
		threshold, want int
	}{{"Alex Example", "Person", 100, 1}, {" alex-example ", "Person", 100, 1}, {"Alex Examplx", "Person", 100, 0}, {"Alex Examplx", "Person", 90, 1}, {"Alex Example", "Organization", 0, 1}, {"Alex Example", "Person", 0, 2}} {
		raw, _ := json.Marshal(map[string]int{"threshold": test.threshold})
		result, e := p.Search(context.Background(), d.SearchRequest{Provider: "opensanctions", Queries: []d.SearchQuery{{Name: test.name, Type: test.kind}}, ProviderConfig: raw})
		if e != nil || len(result.Matches) != test.want {
			t.Fatalf("%+v: matches=%d error=%v", test, len(result.Matches), e)
		}
		if !strings.Contains(string(result.RawResponse), `"synthetic":true`) {
			t.Fatal("unlabelled synthetic result")
		}
	}
	limit := 1
	result, e := p.Search(context.Background(), d.SearchRequest{Provider: "opensanctions", Queries: []d.SearchQuery{{Name: "Alex Example", Type: "Person"}}, ProviderConfig: json.RawMessage(`{"threshold":0}`), LimitOverride: &limit})
	if e != nil || !result.Partial || len(result.Matches) != 1 {
		t.Fatal("bounded result did not indicate truncation")
	}
	if _, e = p.Search(context.Background(), d.SearchRequest{Provider: "opensanctions", Queries: []d.SearchQuery{{Name: "Alex Example", Type: "Person", BirthDate: "1980"}}}); e == nil {
		t.Fatal("unsupported identity field silently ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = p.Search(ctx, d.SearchRequest{}); e == nil {
		t.Fatal("cancellation ignored")
	}
}
