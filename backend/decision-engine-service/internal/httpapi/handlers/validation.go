package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type ValidationHandler struct {
	validationService service.ValidationService
}

func NewValidationHandler(validationService service.ValidationService) ValidationHandler {
	return ValidationHandler{validationService: validationService}
}

func (h ValidationHandler) ValidateIteration(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	result, err := h.validationService.ValidateIteration(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "validate_iteration_failed", "details": err.Error()})
		return
	}

	status := http.StatusOK
	if !result.Valid {
		status = http.StatusUnprocessableEntity
	}
	c.JSON(status, gin.H{"validation": dto.AdaptIterationValidation(result)})
}

func (h ValidationHandler) ListRuleFunctions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"rule_functions": dto.AdaptRuleFunctionCatalog(ast_eval.SupportedFunctionCatalog()),
	})
}
