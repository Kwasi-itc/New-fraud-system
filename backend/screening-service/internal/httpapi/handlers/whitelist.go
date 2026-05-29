package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

type WhitelistHandler struct {
	service service.ScreeningService
}

func NewWhitelistHandler(service service.ScreeningService) WhitelistHandler {
	return WhitelistHandler{service: service}
}

type createWhitelistRequest struct {
	EntityID                     string  `json:"entity_id"`
	ReviewerID                   string  `json:"reviewer_id"`
	UniqueCounterpartyIdentifier *string `json:"unique_counterparty_identifier"`
}

func (h WhitelistHandler) Search(c *gin.Context) {
	var entityID *string
	var counterparty *string
	if value := c.Query("entity_id"); value != "" {
		entityID = &value
	}
	if value := c.Query("unique_counterparty_identifier"); value != "" {
		counterparty = &value
	}

	items, err := h.service.SearchWhitelist(c.Request.Context(), c.Param("tenantId"), entityID, counterparty)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h WhitelistHandler) Create(c *gin.Context) {
	var body createWhitelistRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item, err := h.service.CreateWhitelistEntry(c.Request.Context(), c.Param("tenantId"), body.EntityID, body.ReviewerID, body.UniqueCounterpartyIdentifier)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h WhitelistHandler) Delete(c *gin.Context) {
	entityID := c.Query("entity_id")
	if entityID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "entity_id is required"})
		return
	}

	var counterparty *string
	if value := c.Query("unique_counterparty_identifier"); value != "" {
		counterparty = &value
	}

	if err := h.service.DeleteWhitelistEntry(c.Request.Context(), c.Param("tenantId"), entityID, counterparty); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
