package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

type IngestHandler struct {
	ingestService         service.IngestService
	deferredIngestService service.DeferredIngestService
	writePathOverloadMode string
	writeLimiter          chan struct{}
	readLimiter           chan struct{}
	aggregateLimiter      chan struct{}
	aggregateQueryTimeout time.Duration
}

type IngestHandlerConfig struct {
	WritePathLimiter               chan struct{}
	WritePathOverloadMode          string
	DeferredIngestService          service.DeferredIngestService
	ReadQueryConcurrencyLimit      int
	AggregateQueryConcurrencyLimit int
	AggregateQueryTimeout          time.Duration
}

func NewIngestHandler(ingestService service.IngestService, cfg IngestHandlerConfig) IngestHandler {
	var readLimiter chan struct{}
	if cfg.ReadQueryConcurrencyLimit > 0 {
		readLimiter = make(chan struct{}, cfg.ReadQueryConcurrencyLimit)
	}
	var aggregateLimiter chan struct{}
	if cfg.AggregateQueryConcurrencyLimit > 0 {
		aggregateLimiter = make(chan struct{}, cfg.AggregateQueryConcurrencyLimit)
	}
	return IngestHandler{
		ingestService:         ingestService,
		deferredIngestService: cfg.DeferredIngestService,
		writePathOverloadMode: cfg.WritePathOverloadMode,
		writeLimiter:          cfg.WritePathLimiter,
		readLimiter:           readLimiter,
		aggregateLimiter:      aggregateLimiter,
		aggregateQueryTimeout: cfg.AggregateQueryTimeout,
	}
}

func (h IngestHandler) PostIngest(c *gin.Context) {
	h.ingest(c, ingestion.ModeCreate)
}

func (h IngestHandler) PatchIngest(c *gin.Context) {
	h.ingest(c, ingestion.ModePatch)
}

func (h IngestHandler) PostBatchIngest(c *gin.Context) {
	h.batchIngest(c, ingestion.ModeCreate)
}

func (h IngestHandler) PatchBatchIngest(c *gin.Context) {
	h.batchIngest(c, ingestion.ModePatch)
}

func (h IngestHandler) GetRecord(c *gin.Context) {
	c.Set("metric_object_type", c.Param("objectType"))
	release, ok := h.tryAcquireReadSlot(c, "get_record")
	if !ok {
		return
	}
	defer release()

	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"), "object_type", c.Param("objectType"), "object_id", c.Param("objectId"))
		return
	}

	result, err := h.ingestService.GetRecord(c.Request.Context(), tenantID, c.Param("objectType"), c.Param("objectId"))
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "object_id", c.Param("objectId"))
		return
	}

	logHandlerSuccess(c, "get record completed", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "object_id", c.Param("objectId"))
	c.JSON(http.StatusOK, gin.H{"record": result})
}

func (h IngestHandler) ListRecords(c *gin.Context) {
	c.Set("metric_object_type", c.Param("objectType"))
	release, ok := h.tryAcquireReadSlot(c, "list_records")
	if !ok {
		return
	}
	defer release()

	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"), "object_type", c.Param("objectType"))
		return
	}
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeBadRequest(c, "invalid limit", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "limit", raw)
			return
		}
		limit = parsed
	}
	c.Set("metric_list_limit", limit)

	result, err := h.ingestService.ListRecords(c.Request.Context(), tenantID, c.Param("objectType"), limit)
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "limit", limit)
		return
	}

	logHandlerSuccess(c, "list records completed", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "limit", limit, "count", len(result.Records))
	c.JSON(http.StatusOK, gin.H{"records": result.Records})
}

func (h IngestHandler) QueryRecords(c *gin.Context) {
	c.Set("metric_object_type", c.Param("objectType"))
	release, ok := h.tryAcquireReadSlot(c, "query_records")
	if !ok {
		return
	}
	defer release()

	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"), "object_type", c.Param("objectType"))
		return
	}
	fieldName := c.Query("field")
	if fieldName == "" {
		writeBadRequest(c, "field is required", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"))
		return
	}
	value := c.Query("value")
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeBadRequest(c, "invalid limit", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "field", fieldName, "limit", raw)
			return
		}
		limit = parsed
	}
	c.Set("metric_list_limit", limit)
	result, err := h.ingestService.QueryRecords(c.Request.Context(), tenantID, c.Param("objectType"), fieldName, value, limit)
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "field", fieldName, "limit", limit)
		return
	}
	logHandlerSuccess(c, "query records completed", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "field", fieldName, "limit", limit, "count", len(result.Records))
	c.JSON(http.StatusOK, gin.H{"records": result.Records})
}

func (h IngestHandler) AggregateRecords(c *gin.Context) {
	release, ok := h.tryAcquireAggregateSlot(c)
	if !ok {
		return
	}
	defer release()

	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"))
		return
	}

	var query ingestion.AggregateQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		writeBadRequest(c, "request body must be a JSON object", "tenant_id", tenantID.String())
		return
	}
	if query.ObjectType == "" {
		writeBadRequest(c, "object_type is required", "tenant_id", tenantID.String())
		return
	}
	c.Set("metric_object_type", query.ObjectType)
	if query.Aggregate == "" {
		writeBadRequest(c, "aggregate is required", "tenant_id", tenantID.String(), "object_type", query.ObjectType)
		return
	}
	c.Set("metric_aggregate", query.Aggregate)
	shape := ingestion.MeasureAggregateQueryShape(query)
	c.Set("metric_aggregate_shape", ingestion.NormalizedAggregateQueryShapeKey(query))
	if shape.FilterDepth > 0 {
		c.Set("metric_filter_depth", shape.FilterDepth)
	}
	if err := ingestion.ValidateAggregateQuery(query); err != nil {
		writeBadRequest(c, err.Error(), "tenant_id", tenantID.String(), "object_type", query.ObjectType, "aggregate", query.Aggregate, "field", query.Field, "filter_depth", shape.FilterDepth, "filter_nodes", shape.FilterNodes, "rejected_query_shape", true)
		return
	}

	ctx := c.Request.Context()
	if h.aggregateQueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.aggregateQueryTimeout)
		defer cancel()
	}
	result, err := h.ingestService.AggregateRecords(ctx, tenantID, query)
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", query.ObjectType, "aggregate", query.Aggregate, "field", query.Field, "timeout_ms", h.aggregateQueryTimeout.Milliseconds())
		return
	}
	logHandlerSuccess(c, "aggregate records completed", "tenant_id", tenantID.String(), "object_type", query.ObjectType, "aggregate", query.Aggregate, "field", query.Field, "timeout_ms", h.aggregateQueryTimeout.Milliseconds())
	c.JSON(http.StatusOK, gin.H{"value": result.Value})
}

func (h IngestHandler) ingest(c *gin.Context, mode ingestion.Mode) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"), "object_type", c.Param("objectType"), "mode", mode)
		return
	}

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeBadRequest(c, "request body must be a JSON object", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode)
		return
	}

	release, ok := h.tryAcquireWriteSlot(c, "ingest")
	if !ok {
		if h.writePathOverloadMode == "defer_async" {
			h.deferIngest(c, tenantID, c.Param("objectType"), mode, payload)
		} else {
			writeOverloaded(c, "write_path_overloaded", "write path concurrency limit reached", "operation", "ingest")
		}
		return
	}
	defer release()

	result, validationErrors, err := h.ingestService.Ingest(c.Request.Context(), service.IngestInput{
		TenantID:       tenantID,
		ObjectType:     c.Param("objectType"),
		Mode:           mode,
		Payload:        payload,
		IdempotencyKey: optionalHeader(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode)
		return
	}
	if len(validationErrors) > 0 {
		c.Set("error_category", "validation_failed")
		logHandlerFailure(c, "ingest validation failed", nil, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode, "validation_error_count", len(validationErrors))
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": gin.H{
				"code":       "validation_failed",
				"message":    "payload validation failed",
				"category":   "validation_failed",
				"request_id": requestIDFromContext(c),
			},
			"validation_errors": dto.AdaptValidationErrors(validationErrors),
		})
		return
	}

	logHandlerSuccess(c, "ingest completed", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode, "object_id", result.ObjectID, "action", result.Action, "replayed", result.Replayed, "revision_id", result.RevisionID)
	c.JSON(http.StatusOK, gin.H{
		"result": dto.AdaptIngestResult(result),
	})
}

func (h IngestHandler) deferIngest(c *gin.Context, tenantID uuid.UUID, objectType string, mode ingestion.Mode, payload map[string]any) {
	execution, err := h.deferredIngestService.Create(
		c.Request.Context(),
		tenantID,
		objectType,
		mode,
		payload,
		optionalHeader(c.GetHeader("Idempotency-Key")),
	)
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", objectType, "mode", mode, "overload_mode", h.writePathOverloadMode)
		return
	}

	logHandlerSuccess(c, "ingest deferred due to write path saturation", "tenant_id", tenantID.String(), "object_type", objectType, "mode", mode, "deferred_ingest_id", execution.ID)
	c.JSON(http.StatusAccepted, gin.H{
		"deferred_ingest": gin.H{
			"id":           execution.ID,
			"status":       execution.Status,
			"tenant_id":    execution.TenantID,
			"object_type":  execution.ObjectType,
			"mode":         execution.Mode,
			"requested_at": execution.RequestedAt,
		},
	})
}

func (h IngestHandler) batchIngest(c *gin.Context, mode ingestion.Mode) {
	release, ok := h.tryAcquireWriteSlot(c, "batch_ingest")
	if !ok {
		writeOverloaded(c, "write_path_overloaded", "write path concurrency limit reached", "operation", "batch_ingest")
		return
	}
	defer release()

	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"), "object_type", c.Param("objectType"), "mode", mode)
		return
	}

	var records []map[string]any
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&records); err != nil {
		writeBadRequest(c, "request body must be a JSON array of records", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode)
		return
	}

	results, validationErrors, err := h.ingestService.BatchIngest(c.Request.Context(), service.BatchIngestInput{
		TenantID:       tenantID,
		ObjectType:     c.Param("objectType"),
		Mode:           mode,
		Records:        records,
		IdempotencyKey: optionalHeader(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode, "record_count", len(records))
		return
	}
	if len(validationErrors) > 0 {
		c.Set("error_category", "validation_failed")
		logHandlerFailure(c, "batch ingest validation failed", nil, "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode, "record_count", len(records), "validation_error_count", len(validationErrors))
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": gin.H{
				"code":       "validation_failed",
				"message":    "batch validation failed",
				"category":   "validation_failed",
				"request_id": requestIDFromContext(c),
			},
			"validation_errors": dto.AdaptValidationErrors(validationErrors),
		})
		return
	}

	response := make([]dto.IngestResultResponse, len(results))
	for i, result := range results {
		response[i] = dto.AdaptIngestResult(result)
	}
	logHandlerSuccess(c, "batch ingest completed", "tenant_id", tenantID.String(), "object_type", c.Param("objectType"), "mode", mode, "record_count", len(records), "result_count", len(results))
	c.JSON(http.StatusOK, gin.H{
		"results": response,
	})
}

func optionalHeader(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (h IngestHandler) tryAcquireWriteSlot(c *gin.Context, operation string) (func(), bool) {
	if h.writeLimiter == nil {
		return func() {}, true
	}
	select {
	case h.writeLimiter <- struct{}{}:
		return func() { <-h.writeLimiter }, true
	default:
		return nil, false
	}
}

func (h IngestHandler) tryAcquireReadSlot(c *gin.Context, operation string) (func(), bool) {
	if h.readLimiter == nil {
		return func() {}, true
	}
	select {
	case h.readLimiter <- struct{}{}:
		return func() { <-h.readLimiter }, true
	default:
		writeOverloaded(c, "read_path_overloaded", "read path concurrency limit reached", "operation", operation)
		return nil, false
	}
}

func (h IngestHandler) tryAcquireAggregateSlot(c *gin.Context) (func(), bool) {
	if h.aggregateLimiter == nil {
		return func() {}, true
	}
	select {
	case h.aggregateLimiter <- struct{}{}:
		return func() { <-h.aggregateLimiter }, true
	default:
		writeOverloaded(c, "aggregate_path_overloaded", "aggregate concurrency limit reached", "operation", "aggregate_records", "timeout_ms", h.aggregateQueryTimeout.Milliseconds())
		return nil, false
	}
}
