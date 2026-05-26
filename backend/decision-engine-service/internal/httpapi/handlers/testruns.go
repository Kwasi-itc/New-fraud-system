package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type TestRunHandler struct {
	testRunService service.TestRunService
}

func NewTestRunHandler(testRunService service.TestRunService) TestRunHandler {
	return TestRunHandler{testRunService: testRunService}
}

func (h TestRunHandler) Create(c *gin.Context) {
	var req dto.CreateTestRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.testRunService.Create(c.Request.Context(), tenantID, scenarioID, req.PhantomIterationID, req.ExpiresAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_test_run_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"test_run": dto.AdaptTestRun(item)})
}

func (h TestRunHandler) Get(c *gin.Context) {
	tenantID := c.Param("tenantId")
	testRunID := c.Param("testRunId")
	item, err := h.testRunService.GetByID(c.Request.Context(), tenantID, testRunID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_test_run_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"test_run": dto.AdaptTestRun(item)})
}

func (h TestRunHandler) ListByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.testRunService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_test_runs_failed", "details": err.Error()})
		return
	}
	out := make([]dto.TestRunResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptTestRun(item)
	}
	c.JSON(http.StatusOK, gin.H{"test_runs": out})
}

func (h TestRunHandler) Cancel(c *gin.Context) {
	tenantID := c.Param("tenantId")
	testRunID := c.Param("testRunId")
	item, err := h.testRunService.Cancel(c.Request.Context(), tenantID, testRunID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cancel_test_run_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"test_run": dto.AdaptTestRun(item)})
}

func (h TestRunHandler) Evaluate(c *gin.Context) {
	var req dto.EvaluateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	testRunID := c.Param("testRunId")
	result, err := h.testRunService.Evaluate(c.Request.Context(), tenantID, testRunID, service.DecisionEvaluationRequest{
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "evaluate_test_run_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": dto.AdaptTestRunEvaluation(result)})
}

func (h TestRunHandler) DecisionSummaries(c *gin.Context) {
	tenantID := c.Param("tenantId")
	testRunID := c.Param("testRunId")
	items, err := h.testRunService.DecisionSummaries(c.Request.Context(), tenantID, testRunID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_test_run_decision_summaries_failed", "details": err.Error()})
		return
	}
	out := make([]dto.TestRunDecisionSummaryResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptTestRunDecisionSummary(item)
	}
	c.JSON(http.StatusOK, gin.H{"decisions": out})
}

func (h TestRunHandler) RuleStats(c *gin.Context) {
	tenantID := c.Param("tenantId")
	testRunID := c.Param("testRunId")
	items, err := h.testRunService.RuleStats(c.Request.Context(), tenantID, testRunID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_test_run_rule_stats_failed", "details": err.Error()})
		return
	}
	out := make([]dto.TestRunRuleStatResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptTestRunRuleStat(item)
	}
	c.JSON(http.StatusOK, gin.H{"rules": out})
}
