package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type PublicationHandler struct {
	iterationService   service.IterationService
	publicationService service.PublicationService
}

func NewPublicationHandler(
	iterationService service.IterationService,
	publicationService service.PublicationService,
) PublicationHandler {
	return PublicationHandler{
		iterationService:   iterationService,
		publicationService: publicationService,
	}
}

func (h PublicationHandler) CommitIteration(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	item, err := h.iterationService.Commit(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "commit_iteration_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"iteration": dto.AdaptIteration(item)})
}

func (h PublicationHandler) DeactivateIteration(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	result, err := h.publicationService.Unpublish(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deactivate_iteration_failed", "details": err.Error()})
		return
	}

	out := make([]dto.PublicationResponse, len(result))
	for i, item := range result {
		out[i] = dto.AdaptPublication(item)
	}
	c.JSON(http.StatusOK, gin.H{"publications": out})
}

func (h PublicationHandler) ExecutePublicationAction(c *gin.Context) {
	var req dto.PublicationActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	var (
		result []scenario.Publication
		err    error
	)
	switch req.Action {
	case string(scenario.PublicationActionPublish):
		result, err = h.publicationService.Publish(c.Request.Context(), tenantID, scenarioID, req.IterationID)
	case string(scenario.PublicationActionUnpublish):
		result, err = h.publicationService.Unpublish(c.Request.Context(), tenantID, scenarioID, req.IterationID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_publication_action"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "publication_action_failed", "details": err.Error()})
		return
	}

	out := make([]dto.PublicationResponse, len(result))
	for i, item := range result {
		out[i] = dto.AdaptPublication(item)
	}
	c.JSON(http.StatusOK, gin.H{"publications": out})
}

func (h PublicationHandler) ListPublications(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	items, err := h.publicationService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_publications_failed", "details": err.Error()})
		return
	}

	out := make([]dto.PublicationResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptPublication(item)
	}
	c.JSON(http.StatusOK, gin.H{"publications": out})
}

func (h PublicationHandler) GetPreparationStatus(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Query("iteration_id")
	if iterationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "iteration_id_required"})
		return
	}

	item, err := h.publicationService.GetPreparationStatus(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get_publication_preparation_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"preparation": dto.AdaptPublicationPreparationStatus(item)})
}

func (h PublicationHandler) StartPreparation(c *gin.Context) {
	var req struct {
		IterationID string `json:"iteration_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	if req.IterationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "iteration_id_required"})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.publicationService.StartPreparation(c.Request.Context(), tenantID, scenarioID, req.IterationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "start_publication_preparation_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"preparation": dto.AdaptPublicationPreparationStatus(item)})
}
