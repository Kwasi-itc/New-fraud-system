package datamodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/requestctx"
)

type HTTPClient struct {
	authToken string
	baseURL   string
	client    *http.Client
	cache     *modelCache
}

// WithAuthToken configures the private Data Model integration credential.
func (c HTTPClient) WithAuthToken(token string) HTTPClient { c.authToken = token; return c }

const requestIDHeader = "X-Request-ID"

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return NewHTTPClientWithCacheLimits(baseURL, timeout, defaultCacheEntries, defaultCacheBytes)
}

// Zero capacity disables caching. Each request still revalidates with the origin
// when caching is enabled; the limits do not introduce a stale-data window.
func NewHTTPClientWithCacheLimits(baseURL string, timeout time.Duration, maxEntries, maxBytes int) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		cache:   newModelCache(maxEntries, maxBytes),
		client: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c HTTPClient) GetPublishedDataModel(ctx context.Context, tenantID uuid.UUID) (ingestion.PublishedDataModel, error) {
	url := fmt.Sprintf("%s/v1/tenants/%s/data-model", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("create request: %w", err)
	}
	attachRequestID(req)
	req.Header.Set("Cache-Control", "no-cache")
	cached, hasCached := c.cache.get(tenantID)
	if hasCached {
		req.Header.Set("If-None-Match", cached.etag)
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if !hasCached || resp.Header.Get("ETag") != cached.etag {
			return ingestion.PublishedDataModel{}, fmt.Errorf("invalid data-model-service cache revalidation")
		}
		if strings.Contains(strings.ToLower(resp.Header.Get("Cache-Control")), "no-store") {
			c.cache.forget(tenantID)
		}
		return cloneModel(cached.model), nil
	}

	if resp.StatusCode != http.StatusOK {
		return ingestion.PublishedDataModel{}, fmt.Errorf("unexpected status from data-model-service: %d", resp.StatusCode)
	}

	// Bound response allocation as well as retained cache size.
	const maxResponseBytes = 32 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return ingestion.PublishedDataModel{}, fmt.Errorf("data model exceeds maximum response size")
	}
	var payload getDataModelResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("decode response: %w", err)
	}

	if strings.TrimSpace(payload.DataModel.RevisionID) == "" {
		return ingestion.PublishedDataModel{}, fmt.Errorf("data-model-service response missing revision_id")
	}

	model := ingestion.PublishedDataModel{
		TenantID:            tenantID,
		RevisionID:          payload.DataModel.RevisionID,
		TenantStatus:        payload.DataModel.IngestionContract.TenantStatus,
		Writable:            payload.DataModel.IngestionContract.Writable,
		RecordLookupField:   payload.DataModel.IngestionContract.RecordLookupField,
		PartialUpdates:      payload.DataModel.IngestionContract.PartialUpdates,
		ManagedSystemFields: append([]string(nil), payload.DataModel.IngestionContract.ManagedSystemFields...),
		Tables:              make(map[string]ingestion.ObjectSchema, len(payload.DataModel.Tables)),
	}

	for key, table := range payload.DataModel.Tables {
		fields := make(map[string]ingestion.FieldSchema, len(table.Fields))
		for fieldKey, field := range table.Fields {
			enumValues := make([]ingestion.EnumValue, len(field.EnumValues))
			for i, enumValue := range field.EnumValues {
				enumValues[i] = ingestion.EnumValue{
					ID:    enumValue.ID,
					Value: enumValue.Value,
					Label: enumValue.Label,
				}
			}

			fields[fieldKey] = ingestion.FieldSchema{
				ID:          field.ID,
				Name:        field.Name,
				Description: field.Description,
				DataType:    field.DataType,
				Nullable:    field.Nullable,
				IsEnum:      field.IsEnum,
				IsUnique:    field.IsUnique,
				Archived:    field.Archived,
				EnumValues:  enumValues,
			}
		}

		model.Tables[key] = ingestion.ObjectSchema{
			ID:           table.ID,
			Name:         table.Name,
			Description:  table.Description,
			Alias:        table.Alias,
			SemanticType: table.SemanticType,
			CaptionField: table.CaptionField,
			Archived:     table.Archived,
			Fields:       fields,
		}
	}

	if etag := resp.Header.Get("ETag"); etag != "" && !strings.Contains(strings.ToLower(resp.Header.Get("Cache-Control")), "no-store") && c.cache != nil && c.cache.maxEntries > 0 && len(body) <= c.cache.maxBytes {
		c.cache.put(cacheEntry{tenantID: tenantID, etag: etag, model: cloneModel(model), encodedBytes: len(body)})
	} else {
		c.cache.forget(tenantID)
	}
	return model, nil
}

type getDataModelResponse struct {
	DataModel publishedDataModelResponse `json:"data_model"`
}

type publishedDataModelResponse struct {
	RevisionID        string                            `json:"revision_id"`
	IngestionContract ingestionContractResponse         `json:"ingestion_contract"`
	Tables            map[string]assembledTableResponse `json:"tables"`
}

type ingestionContractResponse struct {
	TenantStatus        string   `json:"tenant_status"`
	Writable            bool     `json:"writable"`
	ManagedSystemFields []string `json:"managed_system_fields"`
	RecordLookupField   string   `json:"record_lookup_field"`
	PartialUpdates      bool     `json:"partial_updates"`
}

type assembledTableResponse struct {
	ID           uuid.UUID                         `json:"id"`
	Name         string                            `json:"name"`
	Description  string                            `json:"description"`
	Alias        string                            `json:"alias"`
	SemanticType string                            `json:"semantic_type"`
	CaptionField string                            `json:"caption_field"`
	Archived     bool                              `json:"archived"`
	Fields       map[string]assembledFieldResponse `json:"fields"`
}

type assembledFieldResponse struct {
	ID          uuid.UUID             `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	DataType    string                `json:"data_type"`
	Nullable    bool                  `json:"nullable"`
	IsEnum      bool                  `json:"is_enum"`
	IsUnique    bool                  `json:"is_unique"`
	Archived    bool                  `json:"archived"`
	EnumValues  []fieldEnumValueModel `json:"enum_values"`
}

type fieldEnumValueModel struct {
	ID    uuid.UUID `json:"id"`
	Value string    `json:"value"`
	Label string    `json:"label"`
}

func attachRequestID(req *http.Request) {
	if req == nil {
		return
	}
	if requestID := requestctx.RequestID(req.Context()); requestID != "" {
		req.Header.Set(requestIDHeader, requestID)
	}
}
