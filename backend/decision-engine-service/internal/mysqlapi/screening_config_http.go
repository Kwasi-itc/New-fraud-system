package mysqlapi

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerScreeningConfigs(v *gin.RouterGroup) {
	p := "/tenants/:tenantId/scenarios/:scenarioId/screening-configs"
	for _, r := range []struct{ method, path, op string }{{"GET", p, "list"}, {"POST", p, "create"}, {"GET", p + "/:configId", "get"}, {"PUT", p + "/:configId", "update"}, {"DELETE", p + "/:configId", "delete"}} {
		v.Handle(r.method, r.path, h.handle("screening-config."+r.op))
	}
}
func (h *ExecutionHTTP) screeningConfig(c *gin.Context, s *mysql.AdministrationStore, op string) (any, int, error) {
	if s == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	ctx := c.Request.Context()
	parent := uuid.MustParse(c.Param("scenarioId"))
	id := uuid.Nil
	if c.Param("configId") != "" {
		id = uuid.MustParse(c.Param("configId"))
	}
	switch op {
	case "list":
		values, err := s.ScreeningConfigs(ctx, parent)
		return gin.H{"screening_configs": adaptHistory(values, dto.AdaptScreeningConfig)}, 200, err
	case "get":
		value, err := s.ScreeningConfig(ctx, parent, id)
		return gin.H{"screening_config": dto.AdaptScreeningConfig(value)}, 200, err
	case "delete":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		return nil, 204, s.DeleteScreeningConfig(ctx, parent, id)
	case "create", "update":
		var req dto.CreateScreeningConfigRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		value, err := s.SaveScreeningConfig(ctx, parent, id, screening.Config{Name: req.Name, AllowedOutcomes: req.AllowedOutcomes, Provider: req.Provider, ConfigJSON: req.ConfigJSON, Active: req.Active, ReviewInboxID: req.ReviewInboxID})
		status := 200
		if op == "create" {
			status = 201
		}
		return gin.H{"screening_config": dto.AdaptScreeningConfig(value)}, status, err
	}
	return nil, 0, errExecutionHTTPBody
}
