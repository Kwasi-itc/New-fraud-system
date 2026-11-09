package handlers

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

type IndexJobHandler struct {
	service    IndexJobOperations
	safeErrors bool
}

// NewStagedIndexJobHandler keeps SQL/provider details out of target HTTP errors.
func NewStagedIndexJobHandler(service IndexJobOperations) IndexJobHandler {
	return IndexJobHandler{service: service, safeErrors: true}
}

func (h IndexJobHandler) operationError(c *gin.Context, err error) {
	if !h.safeErrors {
		writeError(c, err)
		return
	}
	var auth *access.Error
	switch {
	case errors.As(err, &auth):
		c.JSON(auth.Status, gin.H{"error": gin.H{"code": "authorization_failed", "message": auth.Message}})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
	case errors.Is(err, service.ErrIndexJobInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_index_job"}})
	case errors.Is(err, datamodel.ErrIndexJobScopeLimit):
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "scope_limit"}})
	case errors.Is(err, datamodel.ErrIndexJobRetryConflict):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "retry_conflict"}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "index_job_unavailable"}})
	}
}

// IndexJobOperations permits an owner composition to keep validation, mutation
// and durable enqueue in the same transaction before HTTP reports success.
type IndexJobOperations interface {
	ListForTables(context.Context, uuid.UUID, []uuid.UUID, int) ([]datamodel.IndexJob, error)
	Create(context.Context, service.CreateIndexJobInput) (datamodel.IndexJob, error)
	List(context.Context, uuid.UUID) ([]datamodel.IndexJob, error)
	Get(context.Context, uuid.UUID) (datamodel.IndexJob, error)
	Retry(context.Context, uuid.UUID) (datamodel.IndexJob, error)
}

func (h IndexJobHandler) ListScoped(c *gin.Context) {
	tenant, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	q, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
	if queryErr != nil {
		writeBadRequest(c, "invalid query")
		return
	}
	if len(c.Request.URL.RawQuery) > 8192 || len(q) != 2 || len(q["limit"]) != 1 || len(q["table_id"]) == 0 {
		writeBadRequest(c, "table_id and limit required")
		return
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		writeBadRequest(c, "invalid limit")
		return
	}
	tables := make([]uuid.UUID, len(q["table_id"]))
	for i, raw := range q["table_id"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeBadRequest(c, "invalid table_id")
			return
		}
		tables[i] = id
	}
	if err := datamodel.ValidateIndexJobScope(tenant, tables, limit); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	jobs, err := h.service.ListForTables(c.Request.Context(), tenant, tables, limit)
	if err != nil {
		h.operationError(c, err)
		return
	}
	response := make([]dto.IndexJobResponse, len(jobs))
	for i, job := range jobs {
		response[i] = dto.AdaptIndexJob(job)
	}
	c.JSON(http.StatusOK, gin.H{"index_jobs": response})
}

func NewIndexJobHandler(service IndexJobOperations) IndexJobHandler {
	return IndexJobHandler{service: service}
}

func (h IndexJobHandler) Create(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	var request dto.CreateIndexJobRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	indexType, err := datamodel.ParseIndexJobType(request.IndexType)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	if err := datamodel.ValidateIndexJobCreate(indexType, request.Columns); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	job, err := h.service.Create(c.Request.Context(), service.CreateIndexJobInput{
		TenantID:             tenantID,
		TableID:              request.TableID,
		IndexType:            indexType,
		Columns:              request.Columns,
		RequestedByOperation: request.RequestedByOperation,
		ScheduledAt:          request.ScheduledAt,
	})
	if err != nil {
		h.operationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"index_job": dto.AdaptIndexJob(job)})
}

func (h IndexJobHandler) List(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	jobs, err := h.service.List(c.Request.Context(), tenantID)
	if err != nil {
		h.operationError(c, err)
		return
	}
	response := make([]dto.IndexJobResponse, len(jobs))
	for i, job := range jobs {
		response[i] = dto.AdaptIndexJob(job)
	}
	c.JSON(http.StatusOK, gin.H{"index_jobs": response})
}

func (h IndexJobHandler) Get(c *gin.Context) {
	jobID, ok := parseUUIDParam(c, "jobId")
	if !ok {
		return
	}
	job, err := h.service.Get(c.Request.Context(), jobID)
	if err != nil {
		h.operationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"index_job": dto.AdaptIndexJob(job)})
}

func (h IndexJobHandler) Retry(c *gin.Context) {
	jobID, ok := parseUUIDParam(c, "jobId")
	if !ok {
		return
	}
	job, err := h.service.Retry(c.Request.Context(), jobID)
	if err != nil {
		h.operationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"index_job": dto.AdaptIndexJob(job)})
}
