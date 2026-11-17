package providerproduct

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

// Client implements the scoped read contract over authenticated HTTP. A verified
// application scope selects its namespace credential; no caller tenant header,
// global credential fallback, redirects or automatic search retry is used.
type Client struct {
	base string
	keys map[uuid.UUID]string
	http *http.Client
}
type Keyring struct {
	Version int            `json:"version"`
	Clients []KeyringEntry `json:"clients"`
}
type KeyringEntry struct {
	Namespace  uuid.UUID `json:"namespace"`
	Credential string    `json:"credential"`
}

func NewClient(base string, ring Keyring, timeout time.Duration) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || timeout < 10*time.Millisecond || timeout > 30*time.Second || ring.Version != 1 || len(ring.Clients) < 1 || len(ring.Clients) > 1000 {
		return nil, errors.New("invalid standalone provider client configuration")
	}
	keys := map[uuid.UUID]string{}
	for _, entry := range ring.Clients {
		if entry.Namespace == uuid.Nil || keys[entry.Namespace] != "" || len(entry.Credential) != 84 || !strings.HasPrefix(entry.Credential, "spk_") {
			return nil, errors.New("invalid or duplicate provider namespace credential")
		}
		keys[entry.Namespace] = entry.Credential
	}
	return &Client{base: strings.TrimRight(base, "/"), keys: keys, http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// FromEnvironment is opt-in. Existing deployments keep the local provider; a
// malformed HTTP configuration fails startup instead of falling back locally.
func FromEnvironment(timeout time.Duration) (*Client, error) {
	mode := os.Getenv("INTERNAL_PROVIDER_TRANSPORT")
	if mode == "" || mode == "local" {
		if os.Getenv("PROVIDER_HTTP_URL") != "" || os.Getenv("PROVIDER_CLIENT_KEYS_FILE") != "" {
			return nil, errors.New("HTTP provider settings require explicit INTERNAL_PROVIDER_TRANSPORT=http")
		}
		return nil, nil
	}
	if mode != "http" {
		return nil, errors.New("unsupported internal provider transport")
	}
	file, e := os.Open(os.Getenv("PROVIDER_CLIENT_KEYS_FILE"))
	if e != nil {
		return nil, errors.New("provider client keyring unavailable")
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1024*1024 {
		return nil, errors.New("invalid provider client keyring")
	}
	var ring Keyring
	d := json.NewDecoder(io.LimitReader(file, 1024*1024+1))
	d.DisallowUnknownFields()
	if d.Decode(&ring) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid provider client keyring")
	}
	return NewClient(os.Getenv("PROVIDER_HTTP_URL"), ring, timeout)
}

func (c *Client) call(ctx context.Context, s provideraccess.Scope, method, path string, input any) (json.RawMessage, error) {
	if !s.Valid() || s.Tenant() == uuid.Nil || c == nil {
		return nil, provideraccess.ErrScope
	}
	key := c.keys[s.Tenant()]
	if key == "" {
		return nil, provideraccess.ErrScope
	}
	var body io.Reader
	if input != nil {
		raw, e := json.Marshal(input)
		if e != nil {
			return nil, sourcedata.ErrInput
		}
		body = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if e != nil {
		return nil, sourcedata.ErrInput
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	response, e := c.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, sourcedata.ErrNotReady
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
	if e != nil || len(raw) > 8*1024*1024 {
		return nil, sourcedata.ErrNotReady
	}
	switch response.StatusCode {
	case 200:
		if !json.Valid(raw) {
			return nil, sourcedata.ErrNotReady
		}
		return raw, nil
	case 400:
		return nil, sourcedata.ErrInput
	case 401, 403:
		return nil, provideraccess.ErrScope
	case 404:
		return nil, sourcedata.ErrDataset
	case 410:
		return nil, sourcedata.ErrEvidence
	default:
		return nil, sourcedata.ErrNotReady
	}
}

func (c *Client) Search(ctx context.Context, s provideraccess.Scope, r screening.SearchRequest) (screening.ProviderResult, error) {
	selection, e := sourcedata.DecodeSelection(r.ProviderConfig)
	if e != nil || r.Provider != sourcedata.Provider {
		return screening.ProviderResult{}, sourcedata.ErrInput
	}
	for _, ref := range selection.Datasets {
		if !s.AllowsDataset(ref.Scope, ref.ID) {
			return screening.ProviderResult{}, provideraccess.ErrScope
		}
	}
	limit := 10
	if r.LimitOverride != nil {
		limit = *r.LimitOverride
	}
	raw, e := c.call(ctx, s, "POST", "/v1/search", SearchInput{Profile: selection.Profile, Threshold: selection.Threshold, Datasets: selection.Datasets, Queries: r.Queries, Limit: limit})
	if e != nil {
		return screening.ProviderResult{}, e
	}
	var out screening.ProviderResult
	var proof struct {
		Version  int                        `json:"contract_version"`
		Provider string                     `json:"provider"`
		Partial  *bool                      `json:"partial"`
		Matches  *[]json.RawMessage         `json:"matches"`
		Datasets []internalprovider.Dataset `json:"datasets"`
	}
	if json.Unmarshal(raw, &out) != nil || json.Unmarshal(raw, &proof) != nil || proof.Version != 2 || proof.Provider != sourcedata.Provider || proof.Partial == nil || proof.Matches == nil || out.ProviderReference == "" || len(out.Matches) > limit {
		return screening.ProviderResult{}, sourcedata.ErrEvidence
	}
	selected := map[sourcedata.DatasetRef]uuid.UUID{}
	for _, d := range proof.Datasets {
		if !d.Ref.Valid() || d.Generation == nil || selected[d.Ref] != uuid.Nil {
			return screening.ProviderResult{}, sourcedata.ErrEvidence
		}
		selected[d.Ref] = *d.Generation
	}
	if len(selected) != len(selection.Datasets) {
		return screening.ProviderResult{}, sourcedata.ErrEvidence
	}
	for _, ref := range selection.Datasets {
		if selected[ref] == uuid.Nil {
			return screening.ProviderResult{}, sourcedata.ErrEvidence
		}
	}
	for _, match := range out.Matches {
		var payload struct {
			Ref sourcedata.CandidateRef `json:"provenance"`
		}
		if json.Unmarshal(match.Payload, &payload) != nil || selected[payload.Ref.Dataset] == uuid.Nil || selected[payload.Ref.Dataset] != payload.Ref.Generation || payload.Ref.RecordID == "" || match.EntityID == "" || match.Score < 0 || match.Score > 1 {
			return screening.ProviderResult{}, sourcedata.ErrEvidence
		}
	}
	out.RawResponse = raw
	return out, nil
}
func (c *Client) Enrich(ctx context.Context, s provideraccess.Scope, ref sourcedata.CandidateRef) (screening.EnrichmentResult, error) {
	if !s.AllowsDataset(ref.Dataset.Scope, ref.Dataset.ID) {
		return screening.EnrichmentResult{}, provideraccess.ErrScope
	}
	raw, e := c.call(ctx, s, "POST", "/v1/enrich", ref)
	if e != nil {
		return screening.EnrichmentResult{}, e
	}
	var proof struct {
		Supplement struct {
			Ref sourcedata.CandidateRef `json:"provenance"`
		} `json:"supplement"`
	}
	if json.Unmarshal(raw, &proof) != nil || proof.Supplement.Ref != ref {
		return screening.EnrichmentResult{}, sourcedata.ErrEvidence
	}
	return screening.EnrichmentResult{RawPayload: raw}, nil
}
func operationPath(path string, s provideraccess.Scope, op sourcedata.Operation) (string, error) {
	if op.Provider != sourcedata.Provider || len(op.Selection) > 10 || len(op.Cursor) > 100 || op.Limit < 0 || op.Limit > 500 {
		return "", sourcedata.ErrInput
	}
	query := url.Values{}
	for _, ref := range op.Selection {
		if !ref.Valid() {
			return "", sourcedata.ErrInput
		}
		if !s.AllowsDataset(ref.Scope, ref.ID) {
			return "", provideraccess.ErrScope
		}
		query.Add("dataset", ref.Scope+":"+ref.ID)
	}
	if op.Cursor != "" {
		query.Set("cursor", op.Cursor)
	}
	if op.Limit > 0 {
		query.Set("limit", strconv.Itoa(op.Limit))
	}
	return path + "?" + query.Encode(), nil
}
func (c *Client) GetCatalog(ctx context.Context, s provideraccess.Scope, op sourcedata.Operation) (screening.DatasetCatalog, error) {
	path, e := operationPath("/v1/datasets", s, op)
	if e != nil {
		return screening.DatasetCatalog{}, e
	}
	raw, e := c.call(ctx, s, "GET", path, nil)
	if e == nil {
		e = validateCatalog(raw, op.Selection, false)
	}
	return screening.DatasetCatalog{RawPayload: raw}, e
}
func (c *Client) GetFreshness(ctx context.Context, s provideraccess.Scope, op sourcedata.Operation) (screening.DatasetFreshness, error) {
	path, e := operationPath("/v1/freshness", s, op)
	if e != nil {
		return screening.DatasetFreshness{}, e
	}
	raw, e := c.call(ctx, s, "GET", path, nil)
	if e == nil {
		e = validateCatalog(raw, op.Selection, true)
	}
	return screening.DatasetFreshness{RawPayload: raw}, e
}
func (c *Client) GetDatasetDelta(ctx context.Context, s provideraccess.Scope, op sourcedata.Operation) (screening.DatasetDelta, error) {
	path, e := operationPath("/v1/deltas", s, op)
	if e != nil {
		return screening.DatasetDelta{}, e
	}
	raw, e := c.call(ctx, s, "GET", path, nil)
	if e != nil {
		return screening.DatasetDelta{}, e
	}
	var out screening.DatasetDelta
	var proof struct {
		Version      int                `json:"contract_version"`
		Publications *[]json.RawMessage `json:"publications"`
		Changed      *bool              `json:"changed"`
		More         *bool              `json:"has_more"`
	}
	if json.Unmarshal(raw, &out) != nil || json.Unmarshal(raw, &proof) != nil || proof.Version != 2 || proof.Publications == nil || proof.Changed == nil || proof.More == nil || out.NextCursor == "" || (*proof.Changed != (len(*proof.Publications) > 0)) {
		return screening.DatasetDelta{}, sourcedata.ErrCursor
	}
	out.RawPayload = raw
	return out, nil
}
func (c *Client) Visible(ctx context.Context, s provideraccess.Scope) ([]internalprovider.Dataset, error) {
	result, e := c.GetCatalog(ctx, s, sourcedata.Operation{Provider: sourcedata.Provider, Limit: 500})
	if e != nil {
		return nil, e
	}
	var page struct {
		Datasets []internalprovider.Dataset `json:"datasets"`
		Next     string                     `json:"next_cursor"`
	}
	if json.Unmarshal(result.RawPayload, &page) != nil || page.Next != "" {
		return nil, sourcedata.ErrNotReady
	}
	return page.Datasets, nil
}

func validateCatalog(raw json.RawMessage, selection []sourcedata.DatasetRef, complete bool) error {
	var proof struct {
		Version  int                         `json:"contract_version"`
		Provider string                      `json:"provider"`
		Datasets *[]internalprovider.Dataset `json:"datasets"`
	}
	if json.Unmarshal(raw, &proof) != nil || proof.Version != 2 || proof.Provider != sourcedata.Provider || proof.Datasets == nil {
		return sourcedata.ErrEvidence
	}
	requested := map[sourcedata.DatasetRef]bool{}
	for _, ref := range selection {
		requested[ref] = true
	}
	seen := map[sourcedata.DatasetRef]bool{}
	for _, dataset := range *proof.Datasets {
		if !dataset.Ref.Valid() || seen[dataset.Ref] || (len(requested) > 0 && !requested[dataset.Ref]) {
			return sourcedata.ErrEvidence
		}
		seen[dataset.Ref] = true
	}
	if complete && (len(requested) == 0 || len(seen) != len(requested)) {
		return sourcedata.ErrEvidence
	}
	return nil
}
