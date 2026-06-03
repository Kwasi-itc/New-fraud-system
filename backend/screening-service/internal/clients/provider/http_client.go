package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

type HTTPClient struct {
	defaultProvider string
	clients         map[string]singleProviderClient
	openSanctions   openSanctionsClient
}

type singleProviderClient struct {
	baseURL string
	client  *http.Client
}

type OpenSanctionsConfig struct {
	APIHost   string
	AuthMode  string
	APIKey    string
	Scope     string
	Algorithm string
}

type openSanctionsClient struct {
	host      string
	authMode  string
	apiKey    string
	scope     string
	algorithm string
	client    *http.Client
}

func NewHTTPClient(defaultBaseURL string, providerURLs map[string]string, timeout time.Duration, openCfg OpenSanctionsConfig) HTTPClient {
	clients := make(map[string]singleProviderClient, len(providerURLs)+1)
	for key, baseURL := range providerURLs {
		trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
		if trimmed == "" {
			continue
		}
		clients[strings.ToLower(strings.TrimSpace(key))] = singleProviderClient{
			baseURL: trimmed,
			client:  &http.Client{Timeout: timeout},
		}
	}

	defaultClient := singleProviderClient{
		baseURL: strings.TrimRight(defaultBaseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
	defaultProvider := ""
	if defaultClient.baseURL != "" {
		defaultProvider = "default"
		clients[defaultProvider] = defaultClient
	}

	openClient := newOpenSanctionsClient(openCfg, providerURLs, defaultBaseURL, timeout)

	return HTTPClient{
		defaultProvider: defaultProvider,
		clients:         clients,
		openSanctions:   openClient,
	}
}

func newOpenSanctionsClient(cfg OpenSanctionsConfig, providerURLs map[string]string, defaultBaseURL string, timeout time.Duration) openSanctionsClient {
	host := strings.TrimRight(strings.TrimSpace(cfg.APIHost), "/")
	if host == "" {
		if directHost, ok := providerURLs["opensanctions"]; ok {
			host = strings.TrimRight(strings.TrimSpace(directHost), "/")
		}
	}
	if host == "" && strings.TrimSpace(cfg.APIKey) != "" {
		host = "https://api.opensanctions.org"
	}
	return openSanctionsClient{
		host:      host,
		authMode:  strings.ToLower(strings.TrimSpace(cfg.AuthMode)),
		apiKey:    strings.TrimSpace(cfg.APIKey),
		scope:     defaultIfBlank(strings.TrimSpace(cfg.Scope), "default"),
		algorithm: defaultIfBlank(strings.TrimSpace(cfg.Algorithm), "logic-v1"),
		client:    &http.Client{Timeout: timeout},
	}
}

func ParseProviderURLs(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var fromJSON map[string]string
	if strings.HasPrefix(raw, "{") && json.Unmarshal([]byte(raw), &fromJSON) == nil {
		return normalizeProviderURLs(fromJSON)
	}

	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return normalizeProviderURLs(out)
}

func normalizeProviderURLs(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func (c HTTPClient) Search(ctx context.Context, request screening.SearchRequest) (screening.ProviderResult, error) {
	if c.shouldUseOpenSanctions(request.Provider) {
		return c.openSanctions.Search(ctx, request)
	}
	client, err := c.resolveProvider(request.Provider)
	if err != nil {
		return screening.ProviderResult{}, err
	}

	body, err := json.Marshal(request)
	if err != nil {
		return screening.ProviderResult{}, fmt.Errorf("marshal provider request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return screening.ProviderResult{}, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	raw, err := client.do(req)
	if err != nil {
		return screening.ProviderResult{}, err
	}

	var result screening.ProviderResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return screening.ProviderResult{}, fmt.Errorf("decode provider response: %w", err)
	}
	if len(result.RawResponse) == 0 {
		result.RawResponse = raw
	}
	return result, nil
}

func (c HTTPClient) Enrich(ctx context.Context, provider, entityID string) (screening.EnrichmentResult, error) {
	if c.shouldUseOpenSanctions(provider) {
		return c.openSanctions.Enrich(ctx, entityID)
	}
	client, err := c.resolveProvider(provider)
	if err != nil {
		return screening.EnrichmentResult{}, err
	}
	body, err := json.Marshal(map[string]string{
		"provider":  provider,
		"entity_id": entityID,
	})
	if err != nil {
		return screening.EnrichmentResult{}, fmt.Errorf("marshal enrichment request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/enrich", bytes.NewReader(body))
	if err != nil {
		return screening.EnrichmentResult{}, fmt.Errorf("create enrichment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	raw, err := client.do(req)
	if err != nil {
		return screening.EnrichmentResult{}, err
	}

	var result screening.EnrichmentResult
	if err := json.Unmarshal(raw, &result); err != nil {
		result.RawPayload = raw
		return result, nil
	}
	if len(result.RawPayload) == 0 {
		result.RawPayload = raw
	}
	return result, nil
}

func (c HTTPClient) GetCatalog(ctx context.Context, provider string) (screening.DatasetCatalog, error) {
	if c.shouldUseOpenSanctions(provider) {
		return c.openSanctions.GetCatalog(ctx)
	}
	client, err := c.resolveProvider(provider)
	if err != nil {
		return screening.DatasetCatalog{}, err
	}
	raw, err := client.get(ctx, "/catalog", "")
	if err != nil {
		return screening.DatasetCatalog{}, err
	}
	return screening.DatasetCatalog{RawPayload: raw}, nil
}

func (c HTTPClient) GetFreshness(ctx context.Context, provider string) (screening.DatasetFreshness, error) {
	if c.shouldUseOpenSanctions(provider) {
		return c.openSanctions.GetFreshness(ctx)
	}
	client, err := c.resolveProvider(provider)
	if err != nil {
		return screening.DatasetFreshness{}, err
	}
	raw, err := client.get(ctx, "/freshness", "")
	if err != nil {
		return screening.DatasetFreshness{}, err
	}
	return screening.DatasetFreshness{RawPayload: raw}, nil
}

func (c HTTPClient) GetDatasetDelta(ctx context.Context, providerName, cursor string) (screening.DatasetDelta, error) {
	if c.shouldUseOpenSanctions(providerName) {
		return c.openSanctions.GetDatasetDelta(ctx, cursor)
	}
	client, err := c.resolveProvider(providerName)
	if err != nil {
		return screening.DatasetDelta{}, err
	}
	raw, err := client.get(ctx, "/dataset-updates", cursor)
	if err != nil {
		return screening.DatasetDelta{}, err
	}

	var result screening.DatasetDelta
	if err := json.Unmarshal(raw, &result); err != nil {
		return screening.DatasetDelta{RawPayload: raw, Changed: len(raw) > 0}, nil
	}
	if len(result.RawPayload) == 0 {
		result.RawPayload = raw
	}
	return result, nil
}

func (c HTTPClient) shouldUseOpenSanctions(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "opensanctions") && c.openSanctions.isConfigured()
}

func (c HTTPClient) resolveProvider(provider string) (singleProviderClient, error) {
	key := strings.ToLower(strings.TrimSpace(provider))
	if key != "" {
		if client, ok := c.clients[key]; ok && client.baseURL != "" {
			return client, nil
		}
	}
	if c.defaultProvider != "" {
		if client, ok := c.clients[c.defaultProvider]; ok && client.baseURL != "" {
			return client, nil
		}
	}

	known := make([]string, 0, len(c.clients))
	for key := range c.clients {
		if key != "default" {
			known = append(known, key)
		}
	}
	sort.Strings(known)
	return singleProviderClient{}, fmt.Errorf("provider %q is not configured; known providers: %s", provider, strings.Join(known, ","))
}

func (c singleProviderClient) get(ctx context.Context, path, cursor string) ([]byte, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("screening provider url is not configured")
	}
	endpoint := c.baseURL + path
	if cursor != "" {
		endpoint += "?cursor=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	return c.do(req)
}

func (c singleProviderClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute provider request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read provider response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (c openSanctionsClient) isConfigured() bool {
	return c.host != ""
}

func (c openSanctionsClient) Search(ctx context.Context, request screening.SearchRequest) (screening.ProviderResult, error) {
	if !c.isConfigured() {
		return screening.ProviderResult{}, fmt.Errorf("opensanctions provider is not configured")
	}

	payload, queryOrder, err := c.buildSearchPayload(request)
	if err != nil {
		return screening.ProviderResult{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return screening.ProviderResult{}, fmt.Errorf("marshal opensanctions request: %w", err)
	}

	endpoint, err := c.buildMatchURL(request)
	if err != nil {
		return screening.ProviderResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return screening.ProviderResult{}, fmt.Errorf("create opensanctions request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authenticateRequest(req)

	raw, err := c.do(req)
	if err != nil {
		return screening.ProviderResult{}, err
	}
	return c.adaptSearchResponse(raw, queryOrder)
}

func (c openSanctionsClient) Enrich(ctx context.Context, entityID string) (screening.EnrichmentResult, error) {
	if !c.isConfigured() {
		return screening.EnrichmentResult{}, fmt.Errorf("opensanctions provider is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/entities/"+url.PathEscape(entityID)+c.authQuerySuffix(), nil)
	if err != nil {
		return screening.EnrichmentResult{}, fmt.Errorf("create opensanctions enrichment request: %w", err)
	}
	c.authenticateRequest(req)
	raw, err := c.do(req)
	if err != nil {
		return screening.EnrichmentResult{}, err
	}
	return screening.EnrichmentResult{RawPayload: raw}, nil
}

func (c openSanctionsClient) GetCatalog(ctx context.Context) (screening.DatasetCatalog, error) {
	raw, err := c.get(ctx, "/catalog")
	if err != nil {
		return screening.DatasetCatalog{}, err
	}
	return screening.DatasetCatalog{RawPayload: raw}, nil
}

func (c openSanctionsClient) GetFreshness(ctx context.Context) (screening.DatasetFreshness, error) {
	raw, err := c.get(ctx, "/catalog")
	if err != nil {
		return screening.DatasetFreshness{}, err
	}
	return screening.DatasetFreshness{RawPayload: raw}, nil
}

func (c openSanctionsClient) GetDatasetDelta(ctx context.Context, cursor string) (screening.DatasetDelta, error) {
	raw, err := c.get(ctx, "/catalog")
	if err != nil {
		return screening.DatasetDelta{}, err
	}
	var catalog struct {
		Datasets []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"datasets"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return screening.DatasetDelta{RawPayload: raw, Changed: len(raw) > 0}, nil
	}

	current := make(map[string]string, len(catalog.Datasets))
	for _, dataset := range catalog.Datasets {
		if dataset.Name != "" {
			current[dataset.Name] = dataset.Version
		}
	}

	var previous map[string]string
	if strings.TrimSpace(cursor) != "" {
		_ = json.Unmarshal([]byte(cursor), &previous)
	}
	nextCursorBytes, err := json.Marshal(current)
	if err != nil {
		return screening.DatasetDelta{}, fmt.Errorf("marshal opensanctions dataset cursor: %w", err)
	}
	changed := len(previous) > 0 && !stringMapsEqual(previous, current)
	return screening.DatasetDelta{
		RawPayload: raw,
		NextCursor: string(nextCursorBytes),
		HasMore:    false,
		Changed:    changed,
	}, nil
}

func (c openSanctionsClient) get(ctx context.Context, path string) ([]byte, error) {
	if !c.isConfigured() {
		return nil, fmt.Errorf("opensanctions provider is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path+c.authQuerySuffix(), nil)
	if err != nil {
		return nil, fmt.Errorf("create opensanctions request: %w", err)
	}
	c.authenticateRequest(req)
	return c.do(req)
}

func (c openSanctionsClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute opensanctions request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read opensanctions response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("opensanctions returned status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (c openSanctionsClient) buildSearchPayload(request screening.SearchRequest) (map[string]map[string]any, map[string]string, error) {
	queries := make(map[string]any, len(request.Queries))
	queryOrder := make(map[string]string, len(request.Queries))
	for idx, query := range request.Queries {
		name := strings.TrimSpace(query.Name)
		if name == "" {
			continue
		}
		queryID := "q" + strconv.Itoa(idx+1)
		queryOrder[queryID] = name
		queries[queryID] = map[string]any{
			"schema":     normalizeOpenSanctionsSchema(query.Type),
			"properties": map[string][]string{"name": {name}},
		}
	}
	if len(queries) == 0 {
		return nil, nil, fmt.Errorf("opensanctions request requires at least one query")
	}

	payload := map[string]map[string]any{
		"queries": queries,
	}
	return payload, queryOrder, nil
}

func (c openSanctionsClient) buildMatchURL(request screening.SearchRequest) (string, error) {
	scope := c.scope
	config := parseOpenSanctionsProviderConfig(request.ProviderConfig)
	if config.Scope != "" {
		scope = config.Scope
	}
	endpoint := c.host + "/match/" + url.PathEscape(scope)
	params := url.Values{}
	if suffix := c.authQueryValue(); suffix != "" {
		params.Set("api_key", suffix)
	}
	algorithm := c.algorithm
	if config.Algorithm != "" {
		algorithm = config.Algorithm
	}
	if algorithm != "" {
		params.Set("algorithm", algorithm)
	}
	if config.Threshold != nil {
		threshold := fmt.Sprintf("%.2f", float64(*config.Threshold)/100)
		params.Set("threshold", threshold)
		params.Set("cutoff", threshold)
	}
	if request.LimitOverride != nil {
		params.Set("limit", strconv.Itoa(*request.LimitOverride))
	}
	for _, dataset := range config.Datasets {
		dataset = strings.TrimSpace(dataset)
		if dataset != "" {
			params.Add("include_dataset", dataset)
		}
	}
	if len(params) == 0 {
		return endpoint, nil
	}
	return endpoint + "?" + params.Encode(), nil
}

func (c openSanctionsClient) adaptSearchResponse(raw []byte, queryOrder map[string]string) (screening.ProviderResult, error) {
	var response struct {
		Responses map[string]struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Results []json.RawMessage `json:"results"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return screening.ProviderResult{}, fmt.Errorf("decode opensanctions response: %w", err)
	}

	type parsedResult struct {
		ID         string  `json:"id"`
		Caption    string  `json:"caption"`
		Match      bool    `json:"match"`
		Score      float64 `json:"score"`
		Properties struct {
			Name []string `json:"name"`
		} `json:"properties"`
	}

	matchMap := map[string]screening.ProviderMatch{}
	queryMatches := map[string][]string{}
	partial := false
	for queryID, item := range response.Responses {
		matchCount := 0
		for _, rawResult := range item.Results {
			var parsed parsedResult
			if err := json.Unmarshal(rawResult, &parsed); err != nil {
				return screening.ProviderResult{}, fmt.Errorf("decode opensanctions match: %w", err)
			}
			if !parsed.Match {
				continue
			}
			matchCount++
			name := parsed.Caption
			if len(parsed.Properties.Name) > 0 && strings.TrimSpace(parsed.Properties.Name[0]) != "" {
				name = strings.TrimSpace(parsed.Properties.Name[0])
			}
			if _, ok := matchMap[parsed.ID]; !ok {
				matchMap[parsed.ID] = screening.ProviderMatch{
					EntityID: parsed.ID,
					Name:     name,
					Score:    parsed.Score,
					Payload:  rawResult,
				}
			}
			if queryName := strings.TrimSpace(queryOrder[queryID]); queryName != "" {
				queryMatches[parsed.ID] = append(queryMatches[parsed.ID], queryName)
			}
		}
		if item.Total.Value > matchCount {
			partial = true
		}
	}

	matches := make([]screening.ProviderMatch, 0, len(matchMap))
	for entityID, match := range matchMap {
		match.MatchedTexts = dedupeStrings(queryMatches[entityID])
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].EntityID < matches[j].EntityID
		}
		return matches[i].Score > matches[j].Score
	})

	return screening.ProviderResult{
		RawResponse: raw,
		Partial:     partial,
		Matches:     matches,
	}, nil
}

func (c openSanctionsClient) authenticateRequest(req *http.Request) {
	switch c.authMode {
	case "bearer":
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	case "basic":
		if c.apiKey != "" {
			user, password, _ := strings.Cut(c.apiKey, ":")
			req.SetBasicAuth(user, password)
		}
	}
}

func (c openSanctionsClient) authQuerySuffix() string {
	value := c.authQueryValue()
	if value == "" {
		return ""
	}
	return "?api_key=" + url.QueryEscape(value)
}

func (c openSanctionsClient) authQueryValue() string {
	switch c.authMode {
	case "", "saas", "query":
		return c.apiKey
	default:
		return ""
	}
}

type openSanctionsProviderConfig struct {
	Datasets  []string `json:"datasets"`
	Threshold *int     `json:"threshold"`
	Algorithm string   `json:"algorithm"`
	Scope     string   `json:"scope"`
}

func parseOpenSanctionsProviderConfig(raw json.RawMessage) openSanctionsProviderConfig {
	if len(raw) == 0 {
		return openSanctionsProviderConfig{}
	}
	var cfg openSanctionsProviderConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return openSanctionsProviderConfig{}
	}
	cfg.Algorithm = strings.TrimSpace(cfg.Algorithm)
	cfg.Scope = strings.TrimSpace(cfg.Scope)
	return cfg
}

func normalizeOpenSanctionsSchema(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "person", "individual":
		return "Person"
	case "organization", "business", "company", "legalentity":
		return "Organization"
	case "vessel":
		return "Vessel"
	case "airplane", "aircraft":
		return "Airplane"
	case "vehicle":
		return "Vehicle"
	case "":
		return "Person"
	default:
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return "Person"
		}
		return strings.ToUpper(trimmed[:1]) + strings.ToLower(trimmed[1:])
	}
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringMapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}

func defaultIfBlank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
