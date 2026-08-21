package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
)

type DecisionHandler struct {
	decisionService          service.DecisionService
	executionService         service.ExecutionService
	liveDecisionMode         string
	liveAsyncObjectTypes     map[string]struct{}
	liveLimiter              chan struct{}
	liveAsyncFallbackEnabled bool
}

func NewDecisionHandler(decisionService service.DecisionService, executionService service.ExecutionService, liveDecisionMode string, liveAsyncObjectTypes []string, liveDecisionConcurrencyLimit int, liveAsyncFallbackEnabled bool) DecisionHandler {
	var limiter chan struct{}
	if liveDecisionConcurrencyLimit > 0 {
		limiter = make(chan struct{}, liveDecisionConcurrencyLimit)
	}
	normalizedAsyncTypes := make(map[string]struct{}, len(liveAsyncObjectTypes))
	for _, objectType := range liveAsyncObjectTypes {
		trimmed := strings.ToLower(strings.TrimSpace(objectType))
		if trimmed == "" {
			continue
		}
		normalizedAsyncTypes[trimmed] = struct{}{}
	}
	return DecisionHandler{
		decisionService:          decisionService,
		executionService:         executionService,
		liveDecisionMode:         liveDecisionMode,
		liveAsyncObjectTypes:     normalizedAsyncTypes,
		liveLimiter:              limiter,
		liveAsyncFallbackEnabled: liveAsyncFallbackEnabled,
	}
}

func (h DecisionHandler) EvaluateScenario(c *gin.Context) {
	var req dto.EvaluateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBadRequestError(c, "invalid_request", "evaluate scenario request is invalid", err)
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	if h.shouldForceAsyncForObjectType(req.ObjectType) || h.liveDecisionsAsyncOnly() {
		h.deferAsyncScenarioExecution(c, tenantID, scenarioID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
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
		if errors.Is(err, sharedeventstore.ErrAggregationDeferred) {
			h.deferAsyncScenarioExecution(c, tenantID, scenarioID, req.ObjectID, req.ObjectType, req.Fields)
			return
		}
		writeDecisionEvaluationError(c, "evaluate_scenario_failed", "scenario evaluation failed", err, "tenant_id", tenantID, "scenario_id", scenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType)
		return
	}
	logHandlerSuccess(c, "evaluate scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType, "triggered", result.Triggered)
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptDecisionEvaluation(result)})
}

func (h DecisionHandler) CreateDecision(c *gin.Context) {
	var req dto.CreateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBadRequestError(c, "invalid_request", "create decision request is invalid", err)
		return
	}
	tenantID := c.Param("tenantId")
	if h.shouldForceAsyncForObjectType(req.ObjectType) || h.liveDecisionsAsyncOnly() {
		h.deferAsyncScenarioExecution(c, tenantID, req.ScenarioID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
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
		if errors.Is(err, sharedeventstore.ErrAggregationDeferred) {
			h.deferAsyncScenarioExecution(c, tenantID, req.ScenarioID, req.ObjectID, req.ObjectType, req.Fields)
			return
		}
		writeDecisionEvaluationError(c, "create_decision_failed", "decision creation failed", err, "tenant_id", tenantID, "scenario_id", req.ScenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType)
		return
	}
	logHandlerSuccess(c, "create decision completed", "tenant_id", tenantID, "scenario_id", req.ScenarioID, "object_id", req.ObjectID, "object_type", req.ObjectType, "triggered", result.Triggered)
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptDecisionEvaluation(result)})
}

func (h DecisionHandler) CreateAllDecisions(c *gin.Context) {
	var req dto.EvaluateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBadRequestError(c, "invalid_request", "create all decisions request is invalid", err)
		return
	}
	tenantID := c.Param("tenantId")
	if h.shouldForceAsyncForObjectType(req.ObjectType) || h.liveDecisionsAsyncOnly() {
		h.deferAsyncAllScenariosExecution(c, tenantID, req.ObjectID, req.ObjectType, req.Fields)
		return
	}
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
		if errors.Is(err, sharedeventstore.ErrAggregationDeferred) {
			h.deferAsyncAllScenariosExecution(c, tenantID, req.ObjectID, req.ObjectType, req.Fields)
			return
		}
		writeDecisionEvaluationError(c, "create_all_decisions_failed", "multi-scenario evaluation failed", err, "tenant_id", tenantID, "object_id", req.ObjectID, "object_type", req.ObjectType)
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
	filter := decisionListFilterFromQuery(c)
	limit, offset, paginationEnabled, ok := parseLimitOffset(c)
	if !ok {
		return
	}
	includeTotalCount := parseIncludeTotalCount(c)
	cursor, hasCursor, ok := parseDecisionCursor(c)
	if !ok {
		return
	}
	if hasCursor && limit == 0 {
		limit = 50
	}

	if hasCursor {
		result, err := h.decisionService.ListFilteredCursor(c.Request.Context(), tenantID, filter, limit, cursor, includeTotalCount)
		if err != nil {
			logHandlerFailure(c, "list decisions failed", err, "tenant_id", tenantID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(result.Items)
		pagination := buildPagination(limit, 0, len(out), result.HasMore, result.NextCursor, result.TotalCount)
		logHandlerSuccess(c, "list decisions completed", "tenant_id", tenantID, "count", len(out), "limit", limit, "cursor_mode", true, "has_more", result.HasMore, "scenario_id", filter.ScenarioID, "object_type", filter.ObjectType, "object_id", filter.ObjectID, "outcome", filter.Outcome, "search", filter.Search)
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
		return
	}
	if paginationEnabled {
		result, err := h.decisionService.ListFilteredPage(c.Request.Context(), tenantID, filter, limit, offset, includeTotalCount)
		if err != nil {
			logHandlerFailure(c, "list decisions failed", err, "tenant_id", tenantID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(result.Items)
		pagination := buildPagination(limit, offset, len(out), result.HasMore, nil, result.TotalCount)
		logHandlerSuccess(c, "list decisions completed", "tenant_id", tenantID, "count", len(out), "limit", limit, "offset", offset, "has_more", result.HasMore, "scenario_id", filter.ScenarioID, "object_type", filter.ObjectType, "object_id", filter.ObjectID, "outcome", filter.Outcome, "search", filter.Search)
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
		return
	}

	items, err := h.decisionService.ListFiltered(c.Request.Context(), tenantID, filter)
	if err != nil {
		logHandlerFailure(c, "list decisions failed", err, "tenant_id", tenantID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := adaptDecisionList(items)
	logHandlerSuccess(c, "list decisions completed", "tenant_id", tenantID, "count", len(out), "scenario_id", filter.ScenarioID, "object_type", filter.ObjectType, "object_id", filter.ObjectID, "outcome", filter.Outcome, "search", filter.Search)
	totalCount := len(out)
	c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: buildPagination(len(out), 0, len(out), false, nil, &totalCount)})
}

func (h DecisionHandler) CountDecisions(c *gin.Context) {
	tenantID := c.Param("tenantId")
	filter := decisionListFilterFromQuery(c)
	count, err := h.decisionService.CountFiltered(c.Request.Context(), tenantID, filter)
	if err != nil {
		logHandlerFailure(c, "count decisions failed", err, "tenant_id", tenantID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "count_decisions_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "count decisions completed", "tenant_id", tenantID, "count", count, "scenario_id", filter.ScenarioID, "object_type", filter.ObjectType, "object_id", filter.ObjectID, "outcome", filter.Outcome, "search", filter.Search)
	c.JSON(http.StatusOK, gin.H{"count": count})
}

func (h DecisionHandler) ListDecisionsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	limit, offset, paginationEnabled, ok := parseLimitOffset(c)
	if !ok {
		return
	}
	includeTotalCount := parseIncludeTotalCount(c)
	cursor, hasCursor, ok := parseDecisionCursor(c)
	if !ok {
		return
	}
	if hasCursor && limit == 0 {
		limit = 50
	}
	if hasCursor {
		result, err := h.decisionService.ListFilteredCursor(c.Request.Context(), tenantID, service.DecisionListFilter{ScenarioID: scenarioID}, limit, cursor, includeTotalCount)
		if err != nil {
			logHandlerFailure(c, "list decisions by scenario path failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(result.Items)
		pagination := buildPagination(limit, 0, len(out), result.HasMore, result.NextCursor, result.TotalCount)
		logHandlerSuccess(c, "list decisions by scenario path completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out), "limit", limit, "cursor_mode", true, "has_more", result.HasMore)
		c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: pagination})
		return
	}
	if paginationEnabled {
		result, err := h.decisionService.ListByScenarioPage(c.Request.Context(), tenantID, scenarioID, limit, offset, includeTotalCount)
		if err != nil {
			logHandlerFailure(c, "list decisions by scenario path failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := adaptDecisionList(result.Items)
		pagination := buildPagination(limit, offset, len(out), result.HasMore, nil, result.TotalCount)
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
	totalCount := len(out)
	c.JSON(http.StatusOK, dto.DecisionListEnvelope{Decisions: out, Pagination: buildPagination(len(out), 0, len(out), false, nil, &totalCount)})
}

func adaptDecisionList(items []decision.Decision) []dto.DecisionResponse {
	out := make([]dto.DecisionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptDecision(item)
	}
	return out
}

func buildPagination(limit, offset, itemCount int, hasMore bool, nextCursor *string, totalCount *int) dto.PaginationResponse {
	var nextOffset *int
	if hasMore && nextCursor == nil {
		value := offset + limit
		nextOffset = &value
	}
	var totalPages *int
	if totalCount != nil && limit > 0 {
		value := (*totalCount + limit - 1) / limit
		totalPages = &value
	}
	return dto.PaginationResponse{
		Limit:      limit,
		Offset:     offset,
		HasMore:    hasMore,
		NextCursor: nextCursor,
		TotalCount: totalCount,
		TotalPages: totalPages,
		NextOffset: nextOffset,
	}
}

func parseIncludeTotalCount(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query("include_total_count"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func decisionListFilterFromQuery(c *gin.Context) service.DecisionListFilter {
	return service.DecisionListFilter{
		DecisionID:     c.Query("decision_id"),
		ScenarioID:     c.Query("scenario_id"),
		ObjectIDPrefix: c.Query("object_id_prefix"),
		ObjectType:     c.Query("object_type"),
		ObjectID:       c.Query("object_id"),
		Outcome:        normalizeDecisionOutcomeFilter(c.Query("outcome")),
		Search:         strings.TrimSpace(c.Query("search")),
	}
}

func parseDecisionCursor(c *gin.Context) (*ports.DecisionListCursor, bool, bool) {
	rawCursor := strings.TrimSpace(c.Query("cursor"))
	if rawCursor == "" {
		return nil, false, true
	}
	if strings.TrimSpace(c.Query("offset")) != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_pagination", "details": "cursor and offset cannot be used together"})
		return nil, false, false
	}
	cursor, err := service.DecodeDecisionCursor(rawCursor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_cursor", "details": err.Error()})
		return nil, false, false
	}
	return cursor, true, true
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

func normalizeDecisionOutcomeFilter(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "approve":
		return "approve"
	case "block and review", "block_and_review":
		return "block_and_review"
	case "decline":
		return "decline"
	case "review":
		return "review"
	default:
		return strings.TrimSpace(value)
	}
}

func (h DecisionHandler) HandleRecordIngested(c *gin.Context) {
	var req dto.IngestionTriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBadRequestError(c, "invalid_request", "record ingested request is invalid", err)
		return
	}
	mode, ok := normalizeRecordIngestedMode(req.Mode)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"details": "record ingested mode must be one of sync or async",
		})
		return
	}
	tenantID := c.Param("tenantId")
	if h.shouldForceAsyncForObjectType(req.ObjectType) || h.liveDecisionsAsyncOnly() || mode == "async" {
		h.enqueueAsyncAllScenariosExecution(c, tenantID, req.ObjectID, req.ObjectType, req.Fields, req.WaitTimeoutMS, req.CallbackURL)
		return
	}
	release, ok := h.tryAcquireLiveSlot(c)
	if !ok {
		h.enqueueAsyncAllScenariosExecution(c, tenantID, req.ObjectID, req.ObjectType, req.Fields, req.WaitTimeoutMS, req.CallbackURL)
		return
	}
	defer release()

	result, err := h.decisionService.EvaluateAllLiveScenarios(c.Request.Context(), tenantID, service.DecisionEvaluationRequest{
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	})
	if err != nil {
		writeDecisionEvaluationError(c, "record_ingested_processing_failed", "ingestion-triggered evaluation failed", err, "tenant_id", tenantID, "object_id", req.ObjectID, "object_type", req.ObjectType)
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

func (h DecisionHandler) liveDecisionsAsyncOnly() bool {
	return h.liveDecisionMode == "async_only"
}

func (h DecisionHandler) shouldForceAsyncForObjectType(objectType string) bool {
	if len(h.liveAsyncObjectTypes) == 0 {
		return false
	}
	_, ok := h.liveAsyncObjectTypes[strings.ToLower(strings.TrimSpace(objectType))]
	return ok
}

func normalizeRecordIngestedMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "sync":
		return "sync", true
	case "async":
		return "async", true
	default:
		return "", false
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

func (h DecisionHandler) enqueueAsyncAllScenariosExecution(c *gin.Context, tenantID, objectID, objectType string, fields map[string]any, waitTimeoutMS int, callbackURL string) {
	result, err := h.executionService.CreateAsyncDecisionExecution(c.Request.Context(), tenantID, service.AsyncDecisionExecutionRequest{
		ObjectType:    objectType,
		WaitTimeoutMS: waitTimeoutMS,
		CallbackURL:   callbackURL,
		Items: []service.DecisionEvaluationRequest{{
			ObjectID:   objectID,
			ObjectType: objectType,
			Fields:     fields,
		}},
	})
	if err != nil {
		logHandlerFailure(c, "ingestion-triggered async enqueue failed", err, "tenant_id", tenantID, "object_id", objectID, "object_type", objectType)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "record_ingested_async_enqueue_failed", "details": "failed to enqueue async ingestion-triggered execution"})
		return
	}
	logHandlerSuccess(c, "ingestion-triggered decision deferred to async execution", "tenant_id", tenantID, "object_id", objectID, "object_type", objectType, "execution_id", result.Execution.ID)
	c.JSON(http.StatusAccepted, gin.H{"deferred": true, "async_decision_execution": dto.AdaptAsyncDecisionExecution(result.Execution)})
}
