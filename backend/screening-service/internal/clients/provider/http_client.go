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
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

type HTTPClient struct {
	defaultProvider string
	clients         map[string]singleProviderClient
}

type singleProviderClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(defaultBaseURL string, providerURLs map[string]string, timeout time.Duration) HTTPClient {
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

	return HTTPClient{
		defaultProvider: defaultProvider,
		clients:         clients,
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
