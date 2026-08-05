package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/requestctx"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

const requestIDHeader = "X-Request-ID"

type StatusError struct {
	Service    string
	StatusCode int
}

func (e StatusError) Error() string {
	return fmt.Sprintf("unexpected status from %s: %d", e.Service, e.StatusCode)
}

func IsStatusError(err error, service string, statusCode int) bool {
	var target StatusError
	if !errors.As(err, &target) {
		return false
	}
	return target.Service == service && target.StatusCode == statusCode
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
	attachRequestID(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.TenantRecord{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ports.TenantRecord{}, StatusError{Service: "ingestion-service", StatusCode: resp.StatusCode}
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

func (c HTTPClient) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	urlValue := fmt.Sprintf("%s/v1/tenants/%s/records/%s", c.baseURL, tenantID, objectType)
	if encoded := query.Encode(); encoded != "" {
		urlValue += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlValue, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	attachRequestID(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, StatusError{Service: "ingestion-service", StatusCode: resp.StatusCode}
	}

	var payload listRecordsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	records := make([]ports.TenantRecord, len(payload.Records))
	for i, item := range payload.Records {
		records[i] = ports.TenantRecord{
			ObjectID:   item.ObjectID,
			ObjectType: item.ObjectType,
			Fields:     item.Fields,
		}
	}
	return records, nil
}

func (c HTTPClient) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	query := url.Values{}
	query.Set("field", fieldName)
	query.Set("value", value)
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	urlValue := fmt.Sprintf("%s/v1/tenants/%s/records/%s/search?%s", c.baseURL, tenantID, objectType, query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlValue, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	attachRequestID(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, StatusError{Service: "ingestion-service", StatusCode: resp.StatusCode}
	}

	var payload listRecordsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	records := make([]ports.TenantRecord, len(payload.Records))
	for i, item := range payload.Records {
		records[i] = ports.TenantRecord{
			ObjectID:   item.ObjectID,
			ObjectType: item.ObjectType,
			Fields:     item.Fields,
		}
	}
	return records, nil
}

func (c HTTPClient) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	urlValue := fmt.Sprintf("%s/v1/tenants/%s/query/aggregate", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlValue, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	attachRequestID(req)
	startedAt := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Default().Warn("ingestion aggregate request failed",
			"request_id", requestctx.RequestID(ctx),
			"tenant_id", tenantID,
			"object_type", query.ObjectType,
			"aggregate", query.Aggregate,
			"field", query.Field,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Default().Warn("ingestion aggregate request returned non-200",
			"request_id", requestctx.RequestID(ctx),
			"tenant_id", tenantID,
			"object_type", query.ObjectType,
			"aggregate", query.Aggregate,
			"field", query.Field,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"status_code", resp.StatusCode,
		)
		return nil, StatusError{Service: "ingestion-service", StatusCode: resp.StatusCode}
	}

	var payload aggregateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	slog.Default().Info("ingestion aggregate request completed",
		"request_id", requestctx.RequestID(ctx),
		"tenant_id", tenantID,
		"object_type", query.ObjectType,
		"aggregate", query.Aggregate,
		"field", query.Field,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	return payload.Value, nil
}

type getRecordResponse struct {
	Record recordEnvelope `json:"record"`
}

type recordEnvelope struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
}

type listRecordsResponse struct {
	Records []recordEnvelope `json:"records"`
}

type aggregateResponse struct {
	Value any `json:"value"`
}

func attachRequestID(req *http.Request) {
	if req == nil {
		return
	}
	if requestID := requestctx.RequestID(req.Context()); requestID != "" {
		req.Header.Set(requestIDHeader, requestID)
	}
}
