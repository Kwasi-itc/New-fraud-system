package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type DecisionHandler struct {
	decisionService          service.DecisionService
	executionService         service.ExecutionService
	liveLimiter              chan struct{}
	liveAsyncFallbackEnabled bool
}

func NewDecisionHandler(decisionService service.DecisionService, executionService service.ExecutionService, liveDecisionConcurrencyLimit int, liveAsyncFallbackEnabled bool) DecisionHandler {
	var limiter chan struct{}
	if liveDecisionConcurrencyLimit > 0 {
		limiter = make(chan struct{}, liveDecisionConcurrencyLimit)
	}
	return DecisionHandler{
		decisionService:          decisionService,
		executionService:         executionService,
		liveLimiter:              limiter,
		liveAsyncFallbackEnabled: liveAsyncFallbackEnabled,
	}
}

func (h DecisionHandler) EvaluateScenario(c *gin.Context) {
	var req dto.EvaluateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "evaluate scenario request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	release, ok := h.tryAcquireLiveSlot(c)
	if !ok {
		h.deferAsyncScenarioExecution(c, tenantID, scenarioID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
	defer release()

	result, err := h.decisionService.EvaluateScenario(c.Request.Context(), tenantID, scenarioID, service.DecisionEvaluationRequest{
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	})
	if err != nil {
		logHandlerFailure(c, "evaluate scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "evaluate_scenario_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "evaluate scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType, "triggered", result.Triggered)
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptDecisionEvaluation(result)})
}

func (h DecisionHandler) CreateDecision(c *gin.Context) {
	var req dto.CreateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "create decision request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	release, ok := h.tryAcquireLiveSlot(c)
	if !ok {
		h.deferAsyncScenarioExecution(c, tenantID, req.ScenarioID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
	defer release()
	result, err := h.decisionService.EvaluateScenario(c.Request.Context(), tenantID, req.ScenarioID, service.DecisionEvaluationRequest{
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	})
	if err != nil {
		logHandlerFailure(c, "create decision failed", err, "tenant_id", tenantID, "scenario_id", req.ScenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_decision_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "create decision completed", "tenant_id", tenantID, "scenario_id", req.ScenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType, "triggered", result.Triggered)
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptDecisionEvaluation(result)})
}

func (h DecisionHandler) CreateAllDecisions(c *gin.Context) {
	var req dto.EvaluateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "create all decisions request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	release, ok := h.tryAcquireLiveSlot(c)
	if !ok {
		h.deferAsyncAllScenariosExecution(c, tenantID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
	defer release()
	result, err := h.decisionService.EvaluateAllLiveScenarios(c.Request.Context(), tenantID, service.DecisionEvaluationRequest{
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	})
	if err != nil {
		logHandlerFailure(c, "create all decisions failed", err, "tenant_id", tenantID, "object_id", req.ObjectID, "object_type", req.ObjectType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_all_decisions_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "create all decisions completed", "tenant_id", tenantID, "object_id", req.ObjectID, "object_type", req.ObjectType, "result_count", len(result.Results))
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptMultiDecisionEvaluation(result)})
}

func (h DecisionHandler) GetDecision(c *gin.Context) {
	tenantID := c.Param("tenantId")
	decisionID := c.Param("decisionId")
	item, rules, err := h.decisionService.GetDecision(c.Request.Context(), tenantID, decisionID)
	if err != nil {
		logHandlerFailure(c, "get decision failed", err, "tenant_id", tenantID, "decision_id", decisionID)
		c.JSON(http.StatusNotFound, gin.H{"error": "get_decision_failed", "details": err.Error()})
		return
	}
	out := make([]dto.RuleExecutionResponse, len(rules))
	for i, rule := range rules {
		out[i] = dto.AdaptRuleExecution(rule)
	}
	c.JSON(http.StatusOK, gin.H{
		"decision":        dto.AdaptDecisionDetail(item),
		"rule_executions": out,
	})
	logHandlerSuccess(c, "get decision completed", "tenant_id", tenantID, "decision_id", decisionID, "rule_execution_count", len(out))
}

func (h DecisionHandler) ListDecisions(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Query("scenario_id")
	objectType := c.Query("object_type")
	objectID := c.Query("object_id")
	limit, offset, paginationEnabled, ok := parseLimitOffset(c)
	if !ok {
		return
	}

	if scenarioID != "" {
		if paginationEnabled {
			result, pageErr := h.decisionService.ListByScenarioPage(c.Request.Context(), tenantID, scenarioID, limit, offset)
			if pageErr != nil {
				logHandlerFailure(c, "list decisions by scenario failed", pageErr, "tenant_id", tenantID, "scenario_id", scenarioID)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": pageErr.Error()})
				return
			}
			out := adaptDecisionList(result.Items)
			pagination := buildPagination(limit, offset, len(out), result.TotalCount)
			logHandlerSuccess(c, "list decisions by scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out), "limit", limit, "offset", offset, "has_more", result.HasMore)
			c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
			return
		}
		itemsUnpaged, err := h.decisionService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
		if err != nil {
			logHandlerFailure(c, "list decisions by scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(itemsUnpaged)
		logHandlerSuccess(c, "list decisions by scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out))
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: buildPagination(len(out), 0, len(out), len(out))})
		return
	}

	if objectType != "" && objectID != "" {
		if paginationEnabled {
			result, err := h.decisionService.ListByObjectPage(c.Request.Context(), tenantID, objectType, objectID, limit, offset)
			if err != nil {
				logHandlerFailure(c, "list decisions by object failed", err, "tenant_id", tenantID, "object_type", objectType, "object_id", objectID)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
				return
			}
			out := adaptDecisionList(result.Items)
			pagination := buildPagination(limit, offset, len(out), result.TotalCount)
			logHandlerSuccess(c, "list decisions by object completed", "tenant_id", tenantID, "object_type", objectType, "object_id", objectID, "count", len(out), "limit", limit, "offset", offset, "has_more", result.HasMore)
			c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
			return
		}
		items, err := h.decisionService.ListByObject(c.Request.Context(), tenantID, objectType, objectID)
		if err != nil {
			logHandlerFailure(c, "list decisions by object failed", err, "tenant_id", tenantID, "object_type", objectType, "object_id", objectID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(items)
		logHandlerSuccess(c, "list decisions by object completed", "tenant_id", tenantID, "object_type", objectType, "object_id", objectID, "count", len(out))
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: buildPagination(len(out), 0, len(out), len(out))})
		return
	}

	if paginationEnabled {
		result, err := h.decisionService.ListByTenantPage(c.Request.Context(), tenantID, limit, offset)
		if err != nil {
			logHandlerFailure(c, "list decisions by tenant failed", err, "tenant_id", tenantID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(result.Items)
		pagination := buildPagination(limit, offset, len(out), result.TotalCount)
		logHandlerSuccess(c, "list decisions by tenant completed", "tenant_id", tenantID, "count", len(out), "limit", limit, "offset", offset, "has_more", result.HasMore)
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
		return
	}

	items, err := h.decisionService.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		logHandlerFailure(c, "list decisions by tenant failed", err, "tenant_id", tenantID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := adaptDecisionList(items)
	logHandlerSuccess(c, "list decisions by tenant completed", "tenant_id", tenantID, "count", len(out))
	c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: buildPagination(len(out), 0, len(out), len(out))})
}

func (h DecisionHandler) ListDecisionsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	limit, offset, paginationEnabled, ok := parseLimitOffset(c)
	if !ok {
		return
	}
	if paginationEnabled {
		result, err := h.decisionService.ListByScenarioPage(c.Request.Context(), tenantID, scenarioID, limit, offset)
		if err != nil {
			logHandlerFailure(c, "list decisions by scenario path failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(result.Items)
		pagination := buildPagination(limit, offset, len(out), result.TotalCount)
		logHandlerSuccess(c, "list decisions by scenario path completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out), "limit", limit, "offset", offset, "has_more", result.HasMore)
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
		return
	}
	items, err := h.decisionService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		logHandlerFailure(c, "list decisions by scenario path failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := adaptDecisionList(items)
	logHandlerSuccess(c, "list decisions by scenario path completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out))
	c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: buildPagination(len(out), 0, len(out), len(out))})
}

func adaptDecisionList(items []decision.Decision) []dto.DecisionResponse {
	out := make([]dto.DecisionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptDecision(item)
	}
	return out
}

func buildPagination(limit, offset, itemCount, totalCount int) dto.PaginationResponse {
	hasMore := offset+itemCount < totalCount
	var nextOffset *int
	if hasMore {
		value := offset + limit
		nextOffset = &value
	}
	totalPages := 0
	if limit > 0 {
		totalPages = (totalCount + limit - 1) / limit
	}
	return dto.PaginationResponse{
		Limit:      limit,
		Offset:     offset,
		HasMore:    hasMore,
		TotalCount: totalCount,
		TotalPages: totalPages,
		NextOffset: nextOffset,
	}
}

func parseLimitOffset(c *gin.Context) (limit int, offset int, enabled bool, ok bool) {
	rawLimit := c.Query("limit")
	rawOffset := c.Query("offset")
	if rawLimit == "" && rawOffset == "" {
		return 0, 0, false, true
	}
	limit = 50
	offset = 0
	if rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_limit", "details": "limit must be a positive integer"})
			return 0, 0, false, false
		}
		if parsed > 500 {
			parsed = 500
		}
		limit = parsed
	}
	if rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_offset", "details": "offset must be a non-negative integer"})
			return 0, 0, false, false
		}
		offset = parsed
	}
	return limit, offset, true, true
}

func (h DecisionHandler) HandleRecordIngested(c *gin.Context) {
	var req dto.IngestionTriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "record ingested request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	release, ok := h.tryAcquireLiveSlot(c)
	if !ok {
		h.deferAsyncAllScenariosExecution(c, tenantID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
	defer release()

	result, err := h.decisionService.EvaluateAllLiveScenarios(c.Request.Context(), tenantID, service.DecisionEvaluationRequest{
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	})
	if err != nil {
		logHandlerFailure(c, "record ingested processing failed", err, "tenant_id", tenantID, "object_id", req.ObjectID, "object_type", req.ObjectType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "record_ingested_processing_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "record ingested processing completed", "tenant_id", tenantID, "object_id", req.ObjectID, "object_type", req.ObjectType, "result_count", len(result.Results))
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptMultiDecisionEvaluation(result)})
}

func (h DecisionHandler) tryAcquireLiveSlot(c *gin.Context) (func(), bool) {
	if h.liveLimiter == nil {
		return func() {}, true
	}
	select {
	case h.liveLimiter <- struct{}{}:
		return func() { <-h.liveLimiter }, true
	default:
		return nil, false
	}
}

func (h DecisionHandler) deferAsyncScenarioExecution(c *gin.Context, tenantID, scenarioID, objectID, objectType string, fields map[string]any) {
	if !h.liveAsyncFallbackEnabled {
		logHandlerFailure(c, "live decision rejected due to concurrency limit", nil, "tenant_id", tenantID, "object_id", objectID, "object_type", objectType)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":   "live_decision_overloaded",
			"details": "realtime decision concurrency limit reached; retry or use async execution",
		})
		return
	}
	result, err := h.executionService.CreateAsyncDecisionExecution(c.Request.Context(), tenantID, service.AsyncDecisionExecutionRequest{
		ScenarioID: scenarioID,
		ObjectType: objectType,
		Items: []service.DecisionEvaluationRequest{{
			ObjectID:   objectID,
			ObjectType: objectType,
			Fields:     fields,
		}},
	})
	if err != nil {
		logHandlerFailure(c, "live decision async fallback failed", err, "tenant_id", tenantID, "scenario_id", scenarioID, "object_id", objectID, "object_type", objectType)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live_decision_overloaded", "details": "failed to enqueue async fallback execution"})
		return
	}
	logHandlerSuccess(c, "live decision deferred to async execution", "tenant_id", tenantID, "scenario_id", scenarioID, "object_id", objectID, "object_type", objectType, "execution_id", result.Execution.ID)
	c.JSON(http.StatusAccepted, gin.H{"deferred": true, "async_decision_execution": dto.AdaptAsyncDecisionExecution(result.Execution)})
}

func (h DecisionHandler) deferAsyncAllScenariosExecution(c *gin.Context, tenantID, objectID, objectType string, fields map[string]any) {
	if !h.liveAsyncFallbackEnabled {
		logHandlerFailure(c, "live decision rejected due to concurrency limit", nil, "tenant_id", tenantID, "object_id", objectID, "object_type", objectType)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":   "live_decision_overloaded",
			"details": "realtime decision concurrency limit reached; retry or use async execution",
		})
		return
	}
	result, err := h.executionService.CreateAsyncDecisionExecution(c.Request.Context(), tenantID, service.AsyncDecisionExecutionRequest{
		ObjectType: objectType,
		Items: []service.DecisionEvaluationRequest{{
			ObjectID:   objectID,
			ObjectType: objectType,
			Fields:     fields,
		}},
	})
	if err != nil {
		logHandlerFailure(c, "live all-scenarios async fallback failed", err, "tenant_id", tenantID, "object_id", objectID, "object_type", objectType)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live_decision_overloaded", "details": "failed to enqueue async fallback execution"})
		return
	}
	logHandlerSuccess(c, "live all-scenarios decision deferred to async execution", "tenant_id", tenantID, "object_id", objectID, "object_type", objectType, "execution_id", result.Execution.ID)
	c.JSON(http.StatusAccepted, gin.H{"deferred": true, "async_decision_execution": dto.AdaptAsyncDecisionExecution(result.Execution)})
}
