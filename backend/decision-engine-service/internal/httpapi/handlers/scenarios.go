package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type ScenarioHandler struct {
	scenarioService  service.ScenarioService
	iterationService service.IterationService
}

func NewScenarioHandler(
	scenarioService service.ScenarioService,
	iterationService service.IterationService,
) ScenarioHandler {
	return ScenarioHandler{
		scenarioService:  scenarioService,
		iterationService: iterationService,
	}
}

func (h ScenarioHandler) CreateScenario(c *gin.Context) {
	var req dto.CreateScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "create scenario request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	item, err := h.scenarioService.Create(c.Request.Context(), tenantID, req.Name, req.Description, req.TriggerObjectType)
	if err != nil {
		logHandlerFailure(c, "create scenario failed", err, "tenant_id", tenantID, "trigger_object_type", req.TriggerObjectType, "scenario_name", req.Name)
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_scenario_failed", "details": err.Error()})
		return
	}

	logHandlerSuccess(c, "create scenario completed", "tenant_id", tenantID, "scenario_id", item.ID, "trigger_object_type", item.TriggerObjectType)
	c.JSON(http.StatusCreated, gin.H{"scenario": dto.AdaptScenario(item)})
}

func (h ScenarioHandler) ListScenarios(c *gin.Context) {
	tenantID := c.Param("tenantId")
	items, err := h.scenarioService.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		logHandlerFailure(c, "list scenarios failed", err, "tenant_id", tenantID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_scenarios_failed", "details": err.Error()})
		return
	}

	out := make([]dto.ScenarioResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptScenario(item)
	}
	logHandlerSuccess(c, "list scenarios completed", "tenant_id", tenantID, "count", len(out))
	c.JSON(http.StatusOK, gin.H{"scenarios": out})
}

func (h ScenarioHandler) GetScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.scenarioService.GetByID(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		logHandlerFailure(c, "get scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
		c.JSON(http.StatusNotFound, gin.H{"error": "get_scenario_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "get scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID)
	c.JSON(http.StatusOK, gin.H{"scenario": dto.AdaptScenario(item)})
}

func (h ScenarioHandler) UpdateScenario(c *gin.Context) {
	var req dto.UpdateScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logHandlerFailure(c, "update scenario request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.scenarioService.Update(c.Request.Context(), tenantID, scenarioID, req.Name, req.Description, req.TriggerObjectType)
	if err != nil {
		logHandlerFailure(c, "update scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID, "trigger_object_type", req.TriggerObjectType, "scenario_name", req.Name)
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_scenario_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "update scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID, "trigger_object_type", item.TriggerObjectType)
	c.JSON(http.StatusOK, gin.H{"scenario": dto.AdaptScenario(item)})
}

func (h ScenarioHandler) DeleteScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	if err := h.scenarioService.Delete(c.Request.Context(), tenantID, scenarioID); err != nil {
		logHandlerFailure(c, "delete scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "delete_scenario_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "delete scenario completed", "tenant_id", tenantID, "scenario_id", scenarioID)
	c.Status(http.StatusNoContent)
}

func (h ScenarioHandler) CopyScenario(c *gin.Context) {
	var req dto.CopyScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		logHandlerFailure(c, "copy scenario request failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.scenarioService.Copy(c.Request.Context(), tenantID, scenarioID, req.Name)
	if err != nil {
		logHandlerFailure(c, "copy scenario failed", err, "tenant_id", tenantID, "scenario_id", scenarioID, "scenario_name", req.Name)
		c.JSON(http.StatusBadRequest, gin.H{"error": "copy_scenario_failed", "details": err.Error()})
		return
	}
	logHandlerSuccess(c, "copy scenario completed", "tenant_id", tenantID, "source_scenario_id", scenarioID, "scenario_id", item.ID)
	c.JSON(http.StatusCreated, gin.H{"scenario": dto.AdaptScenario(item)})
}

func (h ScenarioHandler) ListLatestRules(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.scenarioService.ListLatestRules(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_latest_rules_failed", "details": err.Error()})
		return
	}
	out := make([]dto.RuleResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptRule(item)
	}
	c.JSON(http.StatusOK, gin.H{"rules": out})
}

func (h ScenarioHandler) DescribeASTWithAI(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, dto.NotImplementedResponse{
		Error:   "ai_not_implemented",
		Details: "AST AI description has not been extracted into decision-engine-service yet",
	})
}

func (h ScenarioHandler) GenerateRuleWithAI(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, dto.NotImplementedResponse{
		Error:   "ai_not_implemented",
		Details: "rule generation has not been extracted into decision-engine-service yet",
	})
}

func (h ScenarioHandler) CreateIteration(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	item, err := h.iterationService.CreateDraft(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_iteration_failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"iteration": dto.AdaptIteration(item)})
}

func (h ScenarioHandler) ListIterations(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	items, err := h.iterationService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_iterations_failed", "details": err.Error()})
		return
	}

	out := make([]dto.IterationResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptIteration(item)
	}
	c.JSON(http.StatusOK, gin.H{"iterations": out})
}

func (h ScenarioHandler) ListIterationMetadata(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	items, err := h.iterationService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_iterations_failed", "details": err.Error()})
		return
	}

	out := make([]dto.MetadataIterationResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptIterationMetadata(item)
	}
	c.JSON(http.StatusOK, gin.H{"iterations": out})
}

func (h ScenarioHandler) GetIteration(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	item, err := h.iterationService.GetByID(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_iteration_failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"iteration": dto.AdaptIteration(item)})
}

func (h ScenarioHandler) CreateDraftFromIteration(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	item, err := h.iterationService.CreateDraftFromIteration(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_iteration_failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"iteration": dto.AdaptIteration(item)})
}

func (h ScenarioHandler) DescribeRuleWithAI(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, dto.NotImplementedResponse{
		Error:   "ai_not_implemented",
		Details: "rule AI description has not been extracted into decision-engine-service yet",
	})
}

func (h ScenarioHandler) UpdateIteration(c *gin.Context) {
	var req dto.UpdateIterationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	item, err := h.iterationService.UpdateDraft(
		c.Request.Context(),
		tenantID,
		scenarioID,
		iterationID,
		req.TriggerFormula,
		req.ScoreReviewThreshold,
		req.ScoreBlockAndReviewThreshold,
		req.ScoreDeclineThreshold,
		req.Schedule,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_iteration_failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"iteration": dto.AdaptIteration(item)})
}
