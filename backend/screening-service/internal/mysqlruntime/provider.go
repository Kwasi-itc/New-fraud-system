package mysqlruntime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"sort"
	"strings"
	"unicode"
)

// LocalProvider matches only the documented synthetic catalog. It performs no
// sanctions lookup, never contacts OpenSanctions and labels every result.
type LocalProvider struct{}
type catalogEntry struct{ ID, Name, Schema string }

var localCatalog = []catalogEntry{{"local-person-001", "Alex Example", "Person"}, {"local-person-002", "Morgan Sample", "Person"}, {"local-company-001", "Example Trading Limited", "Organization"}}

func LocalCapabilities() provider.ManualCapabilities {
	return provider.ManualCapabilities{ID: "opensanctions", Name: "Synthetic development catalog", Adapter: "local-development", Configured: true, Health: "local", EntityTypes: []string{"Person", "Organization"}, Fields: map[string][]string{"Person": {"name"}, "Organization": {"name"}}, ThresholdMin: 0, ThresholdMax: 100, LimitMax: 50, Limitations: []string{"Synthetic names only; no sanctions coverage or OpenSanctions connection.", "Matching uses normalized name edit distance; other identity fields and dataset selection are unsupported."}, ThresholdSemantics: "0-100 normalized name edit similarity", ResultSemantics: "Synthetic catalog results persisted by the screening service", CandidateMetadata: []string{"entity_id", "name", "score", "datasets"}, LiveEnrichment: false}
}
func normalizeName(v string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, v)
}
func similarity(a, b string) float64 {
	x, y := []rune(normalizeName(a)), []rune(normalizeName(b))
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	row := make([]int, len(y)+1)
	for j := range row {
		row[j] = j
	}
	for i, c := range x {
		prev := row[0]
		row[0] = i + 1
		for j, k := range y {
			old := row[j+1]
			cost := 0
			if c != k {
				cost = 1
			}
			row[j+1] = min(row[j]+1, row[j+1]+1, prev+cost)
			prev = old
		}
	}
	return 100 * (1 - float64(row[len(y)])/float64(max(len(x), len(y))))
}
func (LocalProvider) Search(ctx context.Context, req d.SearchRequest) (d.ProviderResult, error) {
	if e := ctx.Err(); e != nil {
		return d.ProviderResult{}, e
	}
	if req.Provider != "opensanctions" && req.Provider != "local-development" {
		return d.ProviderResult{}, errors.New("provider is not configured in local development")
	}
	if len(req.Queries) < 1 || len(req.Queries) > 50 {
		return d.ProviderResult{}, errors.New("local query limit is 1..50")
	}
	var cfg struct {
		Threshold int      `json:"threshold"`
		Datasets  []string `json:"datasets"`
	}
	cfg.Threshold = 80
	if len(req.ProviderConfig) > 0 && json.Unmarshal(req.ProviderConfig, &cfg) != nil {
		return d.ProviderResult{}, errors.New("invalid local matching settings")
	}
	if cfg.Threshold < 0 || cfg.Threshold > 100 || len(cfg.Datasets) > 0 {
		return d.ProviderResult{}, errors.New("unsupported local matching settings")
	}
	limit := 50
	if req.LimitOverride != nil {
		limit = *req.LimitOverride
	}
	if limit < 1 || limit > 50 {
		return d.ProviderResult{}, errors.New("local result limit is 1..50")
	}
	result := d.ProviderResult{ProviderReference: "local-development/catalog-v1", Matches: []d.ProviderMatch{}}
	for _, entry := range localCatalog {
		score := float64(0)
		eligible := false
		for _, q := range req.Queries {
			if len([]rune(q.Name)) > 200 || q.BirthDate != "" || q.Nationality != "" || q.Country != "" || q.RegistrationNumber != "" || q.Address != "" {
				return result, errors.New("local provider supports bounded name queries only")
			}
			if q.Type == "" || q.Type == entry.Schema {
				eligible = true
				score = max(score, similarity(q.Name, entry.Name))
			}
		}
		if eligible && score >= float64(cfg.Threshold) {
			payload, _ := json.Marshal(map[string]any{"id": entry.ID, "caption": entry.Name, "schema": entry.Schema, "datasets": []string{"local-development"}, "properties": map[string][]string{"name": {entry.Name}}, "synthetic": true, "backend": "local-development"})
			result.Matches = append(result.Matches, d.ProviderMatch{EntityID: entry.ID, Name: entry.Name, Score: score / 100, Payload: payload, MatchedTexts: []string{entry.Name}})
		}
	}
	sort.Slice(result.Matches, func(i, j int) bool {
		if result.Matches[i].Score == result.Matches[j].Score {
			return result.Matches[i].EntityID < result.Matches[j].EntityID
		}
		return result.Matches[i].Score > result.Matches[j].Score
	})
	if len(result.Matches) > limit {
		result.Partial = true
		result.Matches = result.Matches[:limit]
	}
	raw, _ := json.Marshal(map[string]any{"backend": "local-development", "synthetic": true, "matches": result.Matches})
	result.RawResponse = raw
	return result, nil
}
func (LocalProvider) Enrich(ctx context.Context, p, entity string) (d.EnrichmentResult, error) {
	if e := ctx.Err(); e != nil {
		return d.EnrichmentResult{}, e
	}
	for _, v := range localCatalog {
		if v.ID == entity {
			raw, _ := json.Marshal(map[string]any{"synthetic": true, "backend": "local-development", "properties": map[string][]string{"notes": {"Synthetic local development entity"}}})
			return d.EnrichmentResult{RawPayload: raw}, nil
		}
	}
	return d.EnrichmentResult{}, errors.New("synthetic entity not found")
}
func (LocalProvider) GetCatalog(context.Context, string) (d.DatasetCatalog, error) {
	b, _ := json.Marshal(map[string]any{"backend": "local-development", "synthetic": true, "datasets": []map[string]any{{"name": "local-development", "entities": len(localCatalog)}}, "entities": localCatalog})
	return d.DatasetCatalog{RawPayload: b}, nil
}
func (LocalProvider) GetFreshness(context.Context, string) (d.DatasetFreshness, error) {
	return d.DatasetFreshness{RawPayload: json.RawMessage(`{"backend":"local-development","synthetic":true,"version":"catalog-v1","freshness":"fixed synthetic fixture"}`)}, nil
}
func (LocalProvider) GetDatasetDelta(ctx context.Context, p, cursor string) (d.DatasetDelta, error) {
	if cursor != "" && cursor != "catalog-v1" {
		return d.DatasetDelta{}, errors.New("unknown local dataset cursor")
	}
	b, _ := json.Marshal(map[string]any{"backend": "local-development", "synthetic": true, "version": "catalog-v1"})
	return d.DatasetDelta{RawPayload: b, NextCursor: "catalog-v1", Changed: cursor == ""}, ctx.Err()
}
