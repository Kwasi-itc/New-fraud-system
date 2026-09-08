package datamodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/requestctx"
	"golang.org/x/sync/singleflight"
)

const tenantModelCacheTTL = 30 * time.Second
const requestIDHeader = "X-Request-ID"

type HTTPClient struct {
	baseURL string
	client  *http.Client
	now     func() time.Time

	mu               sync.RWMutex
	tenantModelCache map[string]cachedTenantModel
	tenantModelGroup singleflight.Group
}

type cachedTenantModel struct {
	model     ports.TenantModel
	expiresAt time.Time
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		now:              time.Now,
		tenantModelCache: make(map[string]cachedTenantModel),
	}
}

func (c *HTTPClient) GetTenantModel(ctx context.Context, tenantID string) (ports.TenantModel, error) {
	if model, ok := c.cachedTenantModel(tenantID); ok {
		return model, nil
	}

	value, err, _ := c.tenantModelGroup.Do(tenantID, func() (any, error) {
		if model, ok := c.cachedTenantModel(tenantID); ok {
			return model, nil
		}

		model, err := c.fetchTenantModel(ctx, tenantID)
		if err != nil {
			return ports.TenantModel{}, err
		}
		c.storeTenantModel(tenantID, model)
		return model, nil
	})
	if err != nil {
		return ports.TenantModel{}, err
	}
	return value.(ports.TenantModel), nil
}

func (c *HTTPClient) cachedTenantModel(tenantID string) (ports.TenantModel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, ok := c.tenantModelCache[tenantID]
	if !ok || !c.now().Before(item.expiresAt) {
		return ports.TenantModel{}, false
	}
	return item.model, true
}

func (c *HTTPClient) storeTenantModel(tenantID string, model ports.TenantModel) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tenantModelCache[tenantID] = cachedTenantModel{
		model:     model,
		expiresAt: c.now().Add(tenantModelCacheTTL),
	}
}

func (c *HTTPClient) AnalyzeStoredField(ctx context.Context, tenantID, tableName, fieldName string) (ports.FieldDistributionSuggestion, error) {
	body, _ := json.Marshal(map[string]string{"table_name": tableName, "field_name": fieldName})
	url := fmt.Sprintf("%s/v1/tenants/%s/distribution/analyze-stored", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ports.FieldDistributionSuggestion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	attachRequestID(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.FieldDistributionSuggestion{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ports.FieldDistributionSuggestion{}, fmt.Errorf("stored distribution analysis returned status %d", resp.StatusCode)
	}
	var payload struct {
		Analysis struct {
			SuggestedCategory     string  `json:"suggested_category"`
			Reason                string  `json:"reason"`
			RowsAnalyzed          int64   `json:"rows_analyzed"`
			NonNullRows           int64   `json:"non_null_rows"`
			DistinctValues        int64   `json:"distinct_values"`
			ExpectedSameValueRows float64 `json:"expected_same_value_rows"`
			PolicyVersion         string  `json:"policy_version"`
		} `json:"analysis"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.FieldDistributionSuggestion{}, err
	}
	return ports.FieldDistributionSuggestion{
		SuggestedCategory: payload.Analysis.SuggestedCategory, Reason: payload.Analysis.Reason,
		RowsAnalyzed: payload.Analysis.RowsAnalyzed, NonNullRows: payload.Analysis.NonNullRows,
		DistinctValues: payload.Analysis.DistinctValues, ExpectedSameValueRows: payload.Analysis.ExpectedSameValueRows,
		PolicyVersion: payload.Analysis.PolicyVersion,
	}, nil
}

var _ ports.DistributionSuggestionReader = (*HTTPClient)(nil)

func (c *HTTPClient) fetchTenantModel(ctx context.Context, tenantID string) (ports.TenantModel, error) {
	url := fmt.Sprintf("%s/v1/tenants/%s/data-model", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ports.TenantModel{}, fmt.Errorf("create request: %w", err)
	}
	attachRequestID(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return ports.TenantModel{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ports.TenantModel{}, fmt.Errorf("unexpected status from data-model-service: %d", resp.StatusCode)
	}

	var payload getDataModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.TenantModel{}, fmt.Errorf("decode response: %w", err)
	}
	if strings.TrimSpace(payload.DataModel.RevisionID) == "" {
		return ports.TenantModel{}, fmt.Errorf("data-model-service response missing revision_id")
	}

	model := ports.TenantModel{
		RevisionID:        payload.DataModel.RevisionID,
		RecordLookupField: payload.DataModel.IngestionContract.RecordLookupField,
		Tables:            make(map[string]ports.TenantModelTable, len(payload.DataModel.Tables)),
	}
	for key, table := range payload.DataModel.Tables {
		fields := make(map[string]ports.TenantModelField, len(table.Fields))
		for fieldKey, field := range table.Fields {
			fields[fieldKey] = ports.TenantModelField{
				Name:                        field.Name,
				Type:                        field.DataType,
				DistributionCategory:        field.DistributionCategory,
				ClassificationSource:        field.ClassificationSource,
				ClassificationPolicyVersion: field.ClassificationPolicyVersion,
				ExpectedSameValueRows:       field.ClassificationEvidence.ExpectedSameValueRows,
			}
		}
		links := make(map[string]ports.TenantModelLink, len(table.LinksToSingle))
		for linkKey, link := range table.LinksToSingle {
			links[linkKey] = ports.TenantModelLink{
				Name:            link.Name,
				ParentTableName: link.ParentTableName,
				ParentFieldName: link.ParentFieldName,
				ChildTableName:  link.ChildTableName,
				ChildFieldName:  link.ChildFieldName,
			}
		}
		model.Tables[key] = ports.TenantModelTable{
			ID:            table.ID,
			Name:          table.Name,
			Fields:        fields,
			LinksToSingle: links,
		}
	}

	return model, nil
}

func (c *HTTPClient) CreateIndexJob(ctx context.Context, tenantID, tableID, indexType string, columns []string, requestedByOperation string) (ports.ManagedIndexJob, error) {
	body, err := json.Marshal(map[string]any{
		"table_id":               tableID,
		"index_type":             indexType,
		"columns":                columns,
		"requested_by_operation": requestedByOperation,
	})
	if err != nil {
		return ports.ManagedIndexJob{}, fmt.Errorf("encode index job request: %w", err)
	}
	url := fmt.Sprintf("%s/v1/tenants/%s/index-jobs", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ports.ManagedIndexJob{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	attachRequestID(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return ports.ManagedIndexJob{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ports.ManagedIndexJob{}, fmt.Errorf(
			"unexpected status from data-model-service index job create: %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var payload struct {
		IndexJob indexJobResponse `json:"index_job"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.ManagedIndexJob{}, fmt.Errorf("decode response: %w", err)
	}
	return ports.ManagedIndexJob{
		ID:        payload.IndexJob.ID,
		TableName: payload.IndexJob.TableName,
		IndexType: payload.IndexJob.IndexType,
		Status:    payload.IndexJob.Status,
		Columns:   payload.IndexJob.Columns,
	}, nil
}

func (c *HTTPClient) ListIndexJobs(ctx context.Context, tenantID string) ([]ports.ManagedIndexJob, error) {
	url := fmt.Sprintf("%s/v1/tenants/%s/index-jobs", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status from data-model-service index jobs list: %d", resp.StatusCode)
	}

	var payload listIndexJobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	items := make([]ports.ManagedIndexJob, len(payload.IndexJobs))
	for i, item := range payload.IndexJobs {
		items[i] = ports.ManagedIndexJob{
			ID:        item.ID,
			TableName: item.TableName,
			IndexType: item.IndexType,
			Status:    item.Status,
			Columns:   item.Columns,
		}
	}
	return items, nil
}

func (c *HTTPClient) RetryIndexJob(ctx context.Context, jobID string) error {
	url := fmt.Sprintf("%s/v1/index-jobs/%s/retry", c.baseURL, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status from data-model-service index job retry: %d", resp.StatusCode)
	}
	return nil
}

type getDataModelResponse struct {
	DataModel publishedDataModelResponse `json:"data_model"`
}

type publishedDataModelResponse struct {
	RevisionID        string                            `json:"revision_id"`
	IngestionContract ingestionContractResponse         `json:"ingestion_contract"`
	Tables            map[string]assembledTableResponse `json:"tables"`
}

type assembledTableResponse struct {
	ID            string                            `json:"id"`
	Name          string                            `json:"name"`
	Fields        map[string]assembledFieldResponse `json:"fields"`
	LinksToSingle map[string]assembledLinkResponse  `json:"links_to_single"`
}

type assembledFieldResponse struct {
	Name                        string                         `json:"name"`
	DataType                    string                         `json:"data_type"`
	DistributionCategory        string                         `json:"distribution_category"`
	ClassificationSource        string                         `json:"classification_source"`
	ClassificationPolicyVersion string                         `json:"classification_policy_version"`
	ClassificationEvidence      classificationEvidenceResponse `json:"classification_evidence"`
}

type classificationEvidenceResponse struct {
	ExpectedSameValueRows float64 `json:"expected_same_value_rows"`
}

type ingestionContractResponse struct {
	RecordLookupField string `json:"record_lookup_field"`
}

type assembledLinkResponse struct {
	Name            string `json:"name"`
	ParentTableName string `json:"parent_table_name"`
	ParentFieldName string `json:"parent_field_name"`
	ChildTableName  string `json:"child_table_name"`
	ChildFieldName  string `json:"child_field_name"`
}

type listIndexJobsResponse struct {
	IndexJobs []indexJobResponse `json:"index_jobs"`
}

type indexJobResponse struct {
	ID        string   `json:"id"`
	TableName string   `json:"table_name"`
	IndexType string   `json:"index_type"`
	Status    string   `json:"status"`
	Columns   []string `json:"columns"`
}

func attachRequestID(req *http.Request) {
	if req == nil {
		return
	}
	if requestID := requestctx.RequestID(req.Context()); requestID != "" {
		req.Header.Set(requestIDHeader, requestID)
	}
}
