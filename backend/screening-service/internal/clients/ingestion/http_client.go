package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (c HTTPClient) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	for _, part := range []string{tenantID, objectType, objectID} {
		if strings.TrimSpace(part) == "" || part == "." || part == ".." {
			return ports.TenantRecord{}, fmt.Errorf("invalid ingestion record identity")
		}
	}
	endpoint := fmt.Sprintf("%s/v1/tenants/%s/records/%s/%s", c.baseURL, url.PathEscape(tenantID), url.PathEscape(objectType), url.PathEscape(objectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ports.TenantRecord{}, fmt.Errorf("could not create ingestion record request")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.TenantRecord{}, fmt.Errorf("ingestion record request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ports.TenantRecord{}, fmt.Errorf("unexpected status from ingestion-service: %d", resp.StatusCode)
	}
	var payload getRecordResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.TenantRecord{}, fmt.Errorf("decode response: %w", err)
	}
	if payload.Record.ObjectID != objectID || payload.Record.ObjectType != objectType {
		return ports.TenantRecord{}, fmt.Errorf("ingestion record identity mismatch")
	}
	return ports.TenantRecord{
		ObjectID:   payload.Record.ObjectID,
		ObjectType: payload.Record.ObjectType,
		Fields:     payload.Record.Fields,
	}, nil
}

type getRecordResponse struct {
	Record recordEnvelope `json:"record"`
}

type recordEnvelope struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
}
