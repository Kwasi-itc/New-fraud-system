package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
)

type IntegrationHandler struct {
	service service.CaseService
}

func NewIntegrationHandler(service service.CaseService) IntegrationHandler {
	return IntegrationHandler{service: service}
}

func (h IntegrationHandler) WorkflowAction(c *gin.Context) {
	var body struct {
		WorkflowExecutionID uuid.UUID       `json:"workflow_execution_id"`
		TenantID            uuid.UUID       `json:"tenant_id"`
		DecisionID          uuid.UUID       `json:"decision_id"`
		ScenarioID          *uuid.UUID      `json:"scenario_id"`
		ActionType          string          `json:"action_type"`
		ActionConfig        json.RawMessage `json:"action_config"`
		CreatedAt           time.Time       `json:"created_at"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	var cfg service.WorkflowActionConfig
	if len(body.ActionConfig) > 0 {
		if err := json.Unmarshal(body.ActionConfig, &cfg); err != nil {
			presentError(c, err)
			return
		}
	}
	item, err := h.service.HandleWorkflowAction(c.Request.Context(), service.WorkflowActionInput{
		WorkflowExecutionID: body.WorkflowExecutionID,
		TenantID:            body.TenantID,
		DecisionID:          body.DecisionID,
		ScenarioID:          body.ScenarioID,
		ActionType:          body.ActionType,
		ActionConfig:        cfg,
	})
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"case": item})
}

func (h IntegrationHandler) ScreeningReviewed(c *gin.Context) {
	var body struct {
		TenantID    uuid.UUID  `json:"tenant_id"`
		ScreeningID uuid.UUID  `json:"screening_id"`
		DecisionID  *uuid.UUID `json:"decision_id"`
		MatchID     string     `json:"match_id"`
		Status      string     `json:"status"`
		ReviewerID  *string    `json:"reviewer_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	if err := h.service.HandleScreeningReviewed(c.Request.Context(), body.TenantID, body.ScreeningID, body.DecisionID, body.MatchID, body.Status, body.ReviewerID); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h IntegrationHandler) ScreeningEvidenceUploaded(c *gin.Context) {
	var body struct {
		TenantID    uuid.UUID `json:"tenant_id"`
		ScreeningID uuid.UUID `json:"screening_id"`
		FileID      uuid.UUID `json:"file_id"`
		UploadedBy  *string   `json:"uploaded_by"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (h IntegrationHandler) NotImplemented(c *gin.Context) {
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "note": "worker-backed parity endpoint is scaffolded"})
}
