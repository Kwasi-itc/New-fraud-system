package datamodel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c HTTPClient) GetTenantModel(ctx context.Context, tenantID string) (ports.TenantModel, error) {
	url := fmt.Sprintf("%s/v1/tenants/%s/data-model", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ports.TenantModel{}, fmt.Errorf("create request: %w", err)
	}

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
				Name: field.Name,
				Type: field.DataType,
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
			Name:          table.Name,
			Fields:        fields,
			LinksToSingle: links,
		}
	}

	return model, nil
}

func (c HTTPClient) ListIndexJobs(ctx context.Context, tenantID string) ([]ports.ManagedIndexJob, error) {
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

func (c HTTPClient) RetryIndexJob(ctx context.Context, jobID string) error {
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
	Name          string                            `json:"name"`
	Fields        map[string]assembledFieldResponse `json:"fields"`
	LinksToSingle map[string]assembledLinkResponse  `json:"links_to_single"`
}

type assembledFieldResponse struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
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
