package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
		client:  &http.Client{Timeout: timeout},
	}
}

func (c HTTPClient) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	url := fmt.Sprintf("%s/v1/tenants/%s/records/%s/%s", c.baseURL, tenantID, objectType, objectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ports.TenantRecord{}, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.TenantRecord{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ports.TenantRecord{}, fmt.Errorf("unexpected status from ingestion-service: %d", resp.StatusCode)
	}
	var payload getRecordResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.TenantRecord{}, fmt.Errorf("decode response: %w", err)
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
