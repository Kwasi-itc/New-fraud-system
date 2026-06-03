package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type DecisionHandler struct {
	decisionService service.DecisionService
}

func NewDecisionHandler(decisionService service.DecisionService) DecisionHandler {
	return DecisionHandler{decisionService: decisionService}
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
		"decision":        dto.AdaptDecision(item),
		"rule_executions": out,
	})
	logHandlerSuccess(c, "get decision completed", "tenant_id", tenantID, "decision_id", decisionID, "rule_execution_count", len(out))
}

func (h DecisionHandler) ListDecisions(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Query("scenario_id")
	objectType := c.Query("object_type")
	objectID := c.Query("object_id")

	if scenarioID != "" {
		items, err := h.decisionService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
		if err != nil {
			logHandlerFailure(c, "list decisions by scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := make([]dto.DecisionResponse, len(items))
		for i, item := range items {
			out[i] = dto.AdaptDecision(item)
		}
		logHandlerSuccess(c, "list decisions by scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out))
		c.JSON(http.StatusOK, gin.H{"decisions": out})
		return
	}

	if objectType != "" && objectID != "" {
		items, err := h.decisionService.ListByObject(c.Request.Context(), tenantID, objectType, objectID)
		if err != nil {
			logHandlerFailure(c, "list decisions by object failed", err, "tenant_id", tenantID, "object_type", objectType, "object_id", objectID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := make([]dto.DecisionResponse, len(items))
		for i, item := range items {
			out[i] = dto.AdaptDecision(item)
		}
		logHandlerSuccess(c, "list decisions by object completed", "tenant_id", tenantID, "object_type", objectType, "object_id", objectID, "count", len(out))
		c.JSON(http.StatusOK, gin.H{"decisions": out})
		return
	}

	items, err := h.decisionService.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		logHandlerFailure(c, "list decisions by tenant failed", err, "tenant_id", tenantID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.DecisionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptDecision(item)
	}
	logHandlerSuccess(c, "list decisions by tenant completed", "tenant_id", tenantID, "count", len(out))
	c.JSON(http.StatusOK, gin.H{"decisions": out})
}

func (h DecisionHandler) ListDecisionsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.decisionService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		logHandlerFailure(c, "list decisions by scenario path failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.DecisionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptDecision(item)
	}
	logHandlerSuccess(c, "list decisions by scenario path completed", "tenant_id", tenantID, "scenario_id", scenarioID, "count", len(out))
	c.JSON(http.StatusOK, gin.H{"decisions": out})
}

func (h DecisionHandler) HandleRecordIngested(c *gin.Context) {
	var req dto.IngestionTriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "record ingested request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")

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
