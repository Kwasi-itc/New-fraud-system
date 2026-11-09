package mysqlpublication

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestLocalWatchlistExactMatchingIsDeterministicAndExplicit(t *testing.T) {
	id := uuid.New()
	raw := []byte(`{"queries":[{"name":"  Example   PERSON ","type":"Person"},{"name":"example person","type":"Person"}],"provider_config":{"mode":"exact","watchlist":[{"id":"a","name":"Example Person","type":"Person"},{"id":"b","name":"Example Person","type":"Company"},{"id":"c","name":"Unrelated","type":"Person"}]}}`)
	first, err := localWatchlistResult(id, raw)
	if err != nil || len(first.Matches) != 1 || first.Matches[0].EntityID != "a" {
		t.Fatal(first, err)
	}
	second, err := localWatchlistResult(id, raw)
	if err != nil || first.Matches[0].ID != second.Matches[0].ID {
		t.Fatal("replay changed match identity", second, err)
	}
	var backend map[string]any
	if json.Unmarshal(first.Response, &backend) != nil || backend["backend"] != "local-development-watchlist" {
		t.Fatal(string(first.Response))
	}
	for _, raw := range []string{
		`{"queries":[{"name":"Example"}],"provider_config":{}}`,
		`{"queries":[],"provider_config":{"watchlist":[]}}`,
		`{"queries":[{"name":"Example"}],"provider_config":{"mode":"fuzzy","watchlist":[]}}`,
		`{"queries":[{"name":"Example"}],"provider_config":{"watchlist":[{"id":"a","name":"One"},{"id":"a","name":"Two"}]}}`,
	} {
		if _, err := localWatchlistResult(id, []byte(raw)); err == nil {
			t.Fatal("accepted invalid watchlist", raw)
		}
	}
	noHits, err := localWatchlistResult(id, []byte(`{"queries":[{"name":"Example"}],"provider_config":{"watchlist":[]}}`))
	if err != nil || len(noHits.Matches) != 0 {
		t.Fatal(noHits, err)
	}
}
