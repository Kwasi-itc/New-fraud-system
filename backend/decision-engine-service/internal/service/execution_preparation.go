package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

var ErrExecutionPreparation = errors.New("invalid or inconsistent execution preparation")
var ErrExecutionPreparationLimit = errors.New("execution preparation exceeds configured bounds")

type ExecutionPreparationLimits struct{ MaxItems, MaxBytes int }

func (l ExecutionPreparationLimits) Validate() error {
	if l.MaxItems < 1 || l.MaxItems > 1000 || l.MaxBytes < 1 || l.MaxBytes > 64<<20 {
		return ErrExecutionPreparationLimit
	}
	return nil
}

// PreparedObjectBatch is an opaque, detached batch, not a queue owner or a
// committed evaluation. It holds one published model for this attempt. Never
// serialize the private prepared pointers into a durable job or accept them
// from an HTTP body; a later attempt must prepare against its current model.
type PreparedObjectBatch struct {
	tenant, objectType string
	model              ports.TenantModel
	items              []DecisionEvaluationRequest
	limits             ExecutionPreparationLimits
}

func (b PreparedObjectBatch) Len() int              { return len(b.items) }
func (b PreparedObjectBatch) ModelRevision() string { return b.model.RevisionID }
func (b PreparedObjectBatch) Model() (ports.TenantModel, error) {
	return CloneExecutionModel(b.model, b.limits.MaxBytes)
}

// CloneExecutionRequests bounds raw caller material before encoding and returns
// detached source requests, WITHOUT private prepared authority or normalization.
// An omitted field map remains a stored-record lookup on a later worker attempt;
// never serialize normalized managed fields as untrusted incoming fields.
func CloneExecutionRequests(items []DecisionEvaluationRequest, limits ExecutionPreparationLimits) ([]DecisionEvaluationRequest, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if len(items) > limits.MaxItems {
		return nil, ErrExecutionPreparationLimit
	}
	remaining, nodes := limits.MaxBytes, 0
	for _, item := range items {
		remaining -= len(item.ObjectID) + len(item.ObjectType)
		if remaining < 0 {
			return nil, ErrExecutionPreparationLimit
		}
		if err := boundExecutionValue(item.Fields, &remaining, &nodes, 0); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	if len(raw) > limits.MaxBytes {
		return nil, ErrExecutionPreparationLimit
	}
	var copy []DecisionEvaluationRequest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&copy); err != nil {
		return nil, err
	}
	return copy, nil
}

// FreshExecutionModel deliberately bypasses the live service's metadata cache.
// Provider IO occurs before any persistence/registry transaction. A missing or
// malformed owner provider is a failure, never a fabricated empty model.
func FreshExecutionModel(ctx context.Context, tenant string, provider ports.DataModelReader, maxBytes int) (ports.TenantModel, error) {
	if tenant == "" || provider == nil {
		return ports.TenantModel{}, ErrExecutionPreparation
	}
	model, err := provider.GetTenantModel(ctx, tenant)
	if err != nil {
		return ports.TenantModel{}, err
	}
	return CloneExecutionModel(model, maxBytes)
}

func CloneExecutionModel(model ports.TenantModel, maxBytes int) (ports.TenantModel, error) {
	if maxBytes < 1 || maxBytes > 64<<20 || model.RevisionID == "" || len(model.Tables) == 0 || len(model.Tables) > 1000 || len(model.ManagedSystemFields) > 1000 {
		return ports.TenantModel{}, ErrExecutionPreparation
	}
	// Bound provider material before JSON cloning; no arbitrary maps/values or
	// custom marshalers are accepted in this typed metadata contract.
	remaining := maxBytes
	charge := func(values ...string) bool {
		for _, v := range values {
			if len(v) > remaining {
				return false
			}
			remaining -= len(v)
		}
		return true
	}
	if !charge(model.RevisionID, model.RecordLookupField) {
		return ports.TenantModel{}, ErrExecutionPreparationLimit
	}
	for _, v := range model.ManagedSystemFields {
		if !charge(v) {
			return ports.TenantModel{}, ErrExecutionPreparationLimit
		}
	}
	nodes := len(model.ManagedSystemFields)
	for name, table := range model.Tables {
		if len(table.Fields) > 1000 || len(table.LinksToSingle) > 1000 {
			return ports.TenantModel{}, ErrExecutionPreparationLimit
		}
		nodes += 1 + len(table.Fields) + len(table.LinksToSingle)
		if nodes > 10000 || !charge(name, table.ID, table.Name, table.PublishedVersion) {
			return ports.TenantModel{}, ErrExecutionPreparationLimit
		}
		for key, f := range table.Fields {
			nodes += len(f.EnumValues)
			if nodes > 10000 || !charge(key, f.Name, f.Type) {
				return ports.TenantModel{}, ErrExecutionPreparationLimit
			}
			for _, v := range f.EnumValues {
				if !charge(v) {
					return ports.TenantModel{}, ErrExecutionPreparationLimit
				}
			}
		}
		for key, link := range table.LinksToSingle {
			if !charge(key, link.Name, link.ParentTableName, link.ParentFieldName, link.ChildTableName, link.ChildFieldName) {
				return ports.TenantModel{}, ErrExecutionPreparationLimit
			}
		}
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return ports.TenantModel{}, err
	}
	if len(raw) > maxBytes {
		return ports.TenantModel{}, ErrExecutionPreparationLimit
	}
	var out ports.TenantModel
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

// PrepareObjectBatch validates EVERY item before returning any batch. Supplied
// values are detached with UseNumber, and source/private prepared pointers are
// discarded. Exact object identities, including trailing spaces, are retained.
// SQL/HTTP stored-record reads happen here, outside owner persistence locks.
func PrepareObjectBatch(ctx context.Context, tenant, objectType string, items []DecisionEvaluationRequest, model ports.TenantModel, reader ports.TenantDataReader, limits ExecutionPreparationLimits) (PreparedObjectBatch, error) {
	if err := limits.Validate(); err != nil {
		return PreparedObjectBatch{}, err
	}
	if tenant == "" || objectType == "" || len(items) == 0 {
		return PreparedObjectBatch{}, ErrExecutionPreparation
	}
	if len(items) > limits.MaxItems {
		return PreparedObjectBatch{}, ErrExecutionPreparationLimit
	}
	model, err := CloneExecutionModel(model, limits.MaxBytes)
	if err != nil {
		return PreparedObjectBatch{}, err
	}
	out := PreparedObjectBatch{tenant: tenant, objectType: objectType, model: model, items: make([]DecisionEvaluationRequest, len(items)), limits: limits}
	validator := payload.NewValidator()
	remaining, nodes := limits.MaxBytes, 0
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return PreparedObjectBatch{}, err
		}
		if item.ObjectType != objectType || strings.TrimSpace(item.ObjectID) == "" {
			return PreparedObjectBatch{}, fmt.Errorf("%w: item %d identity/type is invalid", ErrExecutionPreparation, i)
		}
		if len(item.ObjectID)+len(item.ObjectType) > remaining {
			return PreparedObjectBatch{}, ErrExecutionPreparationLimit
		}
		remaining -= len(item.ObjectID) + len(item.ObjectType)
		if err := boundExecutionValue(item.Fields, &remaining, &nodes, 0); err != nil {
			return PreparedObjectBatch{}, err
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return PreparedObjectBatch{}, err
		}
		if len(raw) > limits.MaxBytes {
			return PreparedObjectBatch{}, ErrExecutionPreparationLimit
		}
		var copy DecisionEvaluationRequest
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&copy); err != nil {
			return PreparedObjectBatch{}, err
		}
		copy.storedSource = item.storedSource
		copy, err = prepareObjectIdentity(ctx, tenant, copy, model, reader, validator, false)
		if err != nil {
			return PreparedObjectBatch{}, fmt.Errorf("item %d: %w", i, err)
		}
		// The stored row and scalar normalization may increase the encoded size.
		body, err := copy.JSONBody()
		if err != nil {
			return PreparedObjectBatch{}, err
		}
		out.items[i] = copy
		if len(body) > limits.MaxBytes {
			return PreparedObjectBatch{}, ErrExecutionPreparationLimit
		}
	}
	// Include normalized fields, identity and model evidence in the FINAL batch
	// budget, not merely the incoming request sizes or one item at a time.
	used := 2
	for _, item := range out.items {
		raw, err := item.JSONBody()
		if err != nil {
			return PreparedObjectBatch{}, err
		}
		used += len(raw) + 1
		if used > limits.MaxBytes {
			return PreparedObjectBatch{}, ErrExecutionPreparationLimit
		}
	}
	return out, nil
}

// PrepareStoredRecordBatch is for records actually returned by the scoped
// candidate reader, not caller-supplied HTTP fields. Managed physical fields
// retain the existing stored-record validation rules.
func PrepareStoredRecordBatch(ctx context.Context, tenant, objectType string, records []ports.TenantRecord, model ports.TenantModel, reader ports.TenantDataReader, limits ExecutionPreparationLimits) (PreparedObjectBatch, error) {
	if err := limits.Validate(); err != nil {
		return PreparedObjectBatch{}, err
	}
	if len(records) > limits.MaxItems {
		return PreparedObjectBatch{}, ErrExecutionPreparationLimit
	}
	items := make([]DecisionEvaluationRequest, len(records))
	for i, record := range records {
		items[i] = DecisionEvaluationRequest{ObjectID: record.ObjectID, ObjectType: record.ObjectType, Fields: record.Fields, storedSource: true}
	}
	return PrepareObjectBatch(ctx, tenant, objectType, items, model, reader, limits)
}

func boundExecutionValue(value any, remaining, nodes *int, depth int) error {
	*nodes++
	if depth > 32 || *nodes > 100000 {
		return ErrExecutionPreparationLimit
	}
	charge := func(n int) error {
		if n > *remaining {
			return ErrExecutionPreparationLimit
		}
		*remaining -= n
		return nil
	}
	switch v := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, time.Time:
		return charge(32)
	case string:
		return charge(len(v) + 2)
	case json.Number:
		return charge(len(v))
	case map[string]any:
		if len(v) > *remaining/2 {
			return ErrExecutionPreparationLimit
		}
		for key, value := range v {
			if err := charge(len(key) + 3); err != nil {
				return err
			}
			if err := boundExecutionValue(value, remaining, nodes, depth+1); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if len(v) > *remaining {
			return ErrExecutionPreparationLimit
		}
		for _, value := range v {
			if err := boundExecutionValue(value, remaining, nodes, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrExecutionPreparation
	}
}
