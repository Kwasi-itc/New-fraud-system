package datamodel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
)

// ContractReader consumes the data-model-owned v1 read views. The legacy reader
// serves only explicit full-model consumers, never failures of the SQL path.
type ContractReader struct {
	store  ports.IngestionContractStore
	legacy ports.DataModelReader
	cache  *modelCache
}

func NewContractReader(store ports.IngestionContractStore, legacy ports.DataModelReader, maxEntries, maxBytes int) *ContractReader {
	return &ContractReader{store: store, legacy: legacy, cache: newModelCache(maxEntries, maxBytes)}
}

func (r *ContractReader) GetPublishedDataModel(ctx context.Context, tenantID uuid.UUID) (ingestion.PublishedDataModel, error) {
	if r.legacy == nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("full model reader not configured; request a table contract")
	}
	return r.legacy.GetPublishedDataModel(ctx, tenantID)
}

func (r *ContractReader) GetPublishedTable(ctx context.Context, tenantID uuid.UUID, objectType string) (ingestion.PublishedDataModel, error) {
	cached, found := r.cache.getTable(tenantID, objectType)
	var knownTable uuid.UUID
	var knownVersion uuid.UUID
	if found {
		knownTable = cached.model.Tables[objectType].ID
		knownVersion, _ = uuid.Parse(cached.etag)
	}
	state, err := r.store.ReadContract(ctx, tenantID, objectType, knownTable, knownVersion)
	if err != nil {
		return ingestion.PublishedDataModel{}, err
	}
	if state.TenantID != tenantID {
		return ingestion.PublishedDataModel{}, fmt.Errorf("contract tenant mismatch")
	}
	model := ingestion.PublishedDataModel{
		TenantID: tenantID, SchemaName: state.SchemaName,
		TenantStatus: state.TenantStatus, Writable: state.TenantStatus == "active",
		RecordLookupField: "object_id", PartialUpdates: true,
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables:              make(map[string]ingestion.ObjectSchema),
		RevisionID:          fmt.Sprintf("contract-v1:%s:%s:%s", state.TableID, state.Version, state.TenantStatus),
	}
	if state.TableID == uuid.Nil {
		return model, nil
	}
	if state.TableName != objectType || state.Version == uuid.Nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("invalid table contract identity")
	}
	if state.Payload == nil {
		if !found || knownTable != state.TableID || knownVersion != state.Version {
			return ingestion.PublishedDataModel{}, fmt.Errorf("contract cache version mismatch")
		}
		model.Tables = cloneModel(cached.model).Tables
		return model, nil
	}
	var table assembledTableResponse
	if err := json.Unmarshal(state.Payload, &table); err != nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("decode table contract: %w", err)
	}
	if table.ID != state.TableID || table.Name != objectType || table.Fields == nil {
		return ingestion.PublishedDataModel{}, fmt.Errorf("invalid table contract payload")
	}
	fields := make(map[string]ingestion.FieldSchema, len(table.Fields))
	for name, field := range table.Fields {
		values := make([]ingestion.EnumValue, len(field.EnumValues))
		for i, value := range field.EnumValues {
			values[i] = ingestion.EnumValue{ID: value.ID, Value: value.Value, Label: value.Label}
		}
		fields[name] = ingestion.FieldSchema{ID: field.ID, Name: field.Name, DataType: field.DataType, Nullable: field.Nullable, IsEnum: field.IsEnum, IsUnique: field.IsUnique, Archived: field.Archived, EnumValues: values}
	}
	model.Tables[objectType] = ingestion.ObjectSchema{ID: table.ID, Name: table.Name, Archived: table.Archived, Fields: fields}
	if r.cache.maxEntries > 0 && len(state.Payload) <= r.cache.maxBytes {
		r.cache.put(cacheEntry{tenantID: tenantID, objectType: objectType, etag: state.Version.String(), model: cloneModel(model), encodedBytes: len(state.Payload)})
	}
	return model, nil
}
