package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// preparedObject is owned by one execution and read-only during scenario fan-out.
type preparedObject struct {
	tenant string
	model  ports.TenantModel
	reader ports.TenantDataReader
}

func prepareObject(ctx context.Context, tenant string, req DecisionEvaluationRequest, model ports.TenantModel, reader ports.TenantDataReader, validator *payload.Validator) (DecisionEvaluationRequest, error) {
	started := time.Now()
	req.ObjectID, req.ObjectType = strings.TrimSpace(req.ObjectID), strings.TrimSpace(req.ObjectType)
	if req.ObjectID == "" || req.ObjectType == "" {
		return req, &payload.Error{Category: "payload_validation_failed", ModelRevision: model.RevisionID, Issues: []payload.Issue{{Field: "object_id/object_type", Code: "missing_required", Message: "object ID and object type are required"}}}
	}
	if table, ok := model.Tables[req.ObjectType]; !ok || table.Archived {
		return req, &payload.Error{Category: "payload_validation_failed", ModelRevision: model.RevisionID, Issues: []payload.Issue{{Field: "object_type", Code: "unknown_object_type", Message: "object type is not active in the tenant model"}}}
	}
	if scoped, ok := reader.(ports.ModelScopedReader); ok {
		reader = scoped.WithModel(tenant, model)
	}
	stored := len(req.Fields) == 0
	if stored {
		if reader == nil {
			return req, fmt.Errorf("tenant data reader is not configured")
		}
		record, err := reader.GetRecord(ctx, tenant, req.ObjectType, req.ObjectID)
		if err != nil {
			return req, err
		}
		if record.ObjectID != req.ObjectID || record.ObjectType != req.ObjectType {
			return req, &payload.Error{Category: "stored_record_invalid", ModelRevision: model.RevisionID, Issues: []payload.Issue{{Field: "object_id", Code: "identity_mismatch", Message: "stored record identity does not match the requested object"}}}
		}
		req.Fields = record.Fields
	}
	fields, err := validator.Validate(tenant, req.ObjectType, req.ObjectID, req.Fields, model, stored || req.storedSource)
	slog.Default().Debug("evaluation object preparation", "duration_us", time.Since(started).Microseconds(), "stored", stored, "valid", err == nil)
	if err != nil {
		return req, err
	}
	req.Fields = fields
	req.prepared = &preparedObject{tenant: tenant, model: model, reader: reader}
	return req, nil
}

func (s DecisionService) prepare(ctx context.Context, tenant string, req DecisionEvaluationRequest) (result DecisionEvaluationRequest, failure error) {
	if req.prepared != nil {
		if req.prepared.tenant != tenant {
			return req, fmt.Errorf("prepared object tenant mismatch")
		}
		return req, nil
	}
	started := time.Now()
	var modelDuration time.Duration
	defer func() { s.preparationMetrics.record(time.Since(started), modelDuration, failure) }()
	modelStarted := time.Now()
	model, err := s.getTenantModel(ctx, tenant)
	modelDuration = time.Since(modelStarted)
	if err != nil {
		return req, err
	}
	validator := s.payloadValidator
	if validator == nil {
		validator = payload.NewValidator()
	}
	return prepareObject(ctx, tenant, req, model, s.tenantDataReader, validator)
}

type preparationMetrics struct {
	mu          sync.Mutex
	operation   operationMetricsState
	modelMicros int64
}

type PreparationMetrics struct {
	Operation                  operationMetricsSnapshot `json:"operation"`
	ModelResolutionTotalMicros int64                    `json:"model_resolution_total_micros"`
}

func (m *preparationMetrics) record(total, model time.Duration, err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	recordOperationMetric(&m.operation, total, false, err)
	m.modelMicros += model.Microseconds()
}

func (m *preparationMetrics) snapshot() PreparationMetrics {
	if m == nil {
		return PreparationMetrics{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return PreparationMetrics{snapshotOperationMetrics(m.operation), m.modelMicros}
}
