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
		c.JSON(http.StatusBadRequest, gin.H{"error": "evaluate_scenario_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptDecisionEvaluation(result)})
}

func (h DecisionHandler) CreateDecision(c *gin.Context) {
	var req dto.CreateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_decision_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptDecisionEvaluation(result)})
}

func (h DecisionHandler) CreateAllDecisions(c *gin.Context) {
	var req dto.EvaluateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_all_decisions_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptMultiDecisionEvaluation(result)})
}

func (h DecisionHandler) GetDecision(c *gin.Context) {
	tenantID := c.Param("tenantId")
	decisionID := c.Param("decisionId")
	item, rules, err := h.decisionService.GetDecision(c.Request.Context(), tenantID, decisionID)
	if err != nil {
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
}

func (h DecisionHandler) ListDecisions(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Query("scenario_id")
	objectType := c.Query("object_type")
	objectID := c.Query("object_id")

	if scenarioID != "" {
		items, err := h.decisionService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := make([]dto.DecisionResponse, len(items))
		for i, item := range items {
			out[i] = dto.AdaptDecision(item)
		}
		c.JSON(http.StatusOK, gin.H{"decisions": out})
		return
	}

	if objectType != "" && objectID != "" {
		items, err := h.decisionService.ListByObject(c.Request.Context(), tenantID, objectType, objectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
			return
		}
		out := make([]dto.DecisionResponse, len(items))
		for i, item := range items {
			out[i] = dto.AdaptDecision(item)
		}
		c.JSON(http.StatusOK, gin.H{"decisions": out})
		return
	}

	items, err := h.decisionService.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.DecisionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptDecision(item)
	}
	c.JSON(http.StatusOK, gin.H{"decisions": out})
}

func (h DecisionHandler) ListDecisionsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.decisionService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_decisions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.DecisionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptDecision(item)
	}
	c.JSON(http.StatusOK, gin.H{"decisions": out})
}

func (h DecisionHandler) HandleRecordIngested(c *gin.Context) {
	var req dto.IngestionTriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "record_ingested_processing_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptMultiDecisionEvaluation(result)})
}
