package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

type InternalDecisionHandler struct {
	service service.ScreeningService
}

func NewInternalDecisionHandler(service service.ScreeningService) InternalDecisionHandler {
	return InternalDecisionHandler{service: service}
}

func (h InternalDecisionHandler) Create(c *gin.Context) {
	var body createScreeningRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	idempotencyKey := body.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = c.GetHeader("Idempotency-Key")
	}
	if idempotencyKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "idempotency_key is required"})
		return
	}
	if body.DecisionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decision_id is required"})
		return
	}
	if body.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
		return
	}

	request := screening.SearchRequest{
		Provider:                     body.Provider,
		DecisionID:                   body.DecisionID,
		ScenarioID:                   body.ScenarioID,
		ScreeningConfigID:            body.ScreeningConfigID,
		IdempotencyKey:               idempotencyKey,
		ObjectType:                   body.ObjectType,
		ObjectID:                     body.ObjectID,
		Queries:                      body.Queries,
		LimitOverride:                body.LimitOverride,
		UniqueCounterpartyIdentifier: body.UniqueCounterpartyIdentifier,
	}
	if body.ProviderConfig != nil {
		raw, err := jsonMarshal(body.ProviderConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		request.ProviderConfig = raw
	}

	item, err := h.service.CreateScreening(c.Request.Context(), c.Param("tenantId"), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}
