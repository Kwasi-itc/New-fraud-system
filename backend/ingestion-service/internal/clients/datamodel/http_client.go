package datamodel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
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

func (c HTTPClient) GetPublishedDataModel(ctx context.Context, tenantID uuid.UUID) (ingestion.PublishedDataModel, error) {
	url := fmt.Sprintf("%s/v1/tenants/%s/data-model", c.baseURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ingestion.PublishedDataModel{}, fmt.Errorf("unexpected status from data-model-service: %d", resp.StatusCode)
	}

	var payload getDataModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
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
