package mysqlapi

import (
	"encoding/json"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerScoring(v1 *gin.RouterGroup) {
	path := "/tenants/:tenantId/scenarios/:scenarioId/scoring-configs"
	v1.GET(path, h.handle("scoring.list"))
	v1.POST(path, h.handle("scoring.create"))
	v1.GET(path+"/:configId", h.handle("scoring.get"))
	v1.PUT(path+"/:configId", h.handle("scoring.update"))
	v1.DELETE(path+"/:configId", h.handle("scoring.delete"))
}

// Pointer elements prevent JSON null from silently becoming an empty outcome.
// The wire fields match the source scoring configuration DTO.
type scoringConfigRequest struct {
	Name            string          `json:"name"`
	AllowedOutcomes []*string       `json:"allowed_outcomes"`
	RulesetRef      string          `json:"ruleset_ref"`
	ConfigJSON      json.RawMessage `json:"config_json"`
	Active          bool            `json:"active"`
}

func (h *ExecutionHTTP) scoring(c *gin.Context, a *mysqlexecution.ScoringAuthoring, op string) (out any, status int, err error) {
	defer func() {
		if conflict, ok := err.(*mysqldriver.MySQLError); ok && (conflict.Number == 1062 || conflict.Number == 1451) {
			err = mysql.ErrOwnerRelationshipConflict
		}
	}()
	if a == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	ctx := c.Request.Context()
	parent := uuid.MustParse(c.Param("scenarioId"))
	id := uuid.Nil
	if c.Param("configId") != "" {
		id = uuid.MustParse(c.Param("configId"))
	}
	switch op {
	case "scoring.list":
		values, err := a.List(ctx, parent)
		if err != nil {
			return nil, 0, err
		}
		out := make([]dto.ScoringConfigResponse, len(values))
		for i, v := range values {
			out[i] = dto.AdaptScoringConfig(v)
		}
		return gin.H{"scoring_configs": out}, http.StatusOK, nil
	case "scoring.get":
		v, err := a.Get(ctx, parent, id)
		return gin.H{"scoring_config": dto.AdaptScoringConfig(v)}, http.StatusOK, err
	case "scoring.delete":
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, 0, err
		}
		return nil, http.StatusNoContent, a.Delete(ctx, parent, id)
	case "scoring.create", "scoring.update":
		var req scoringConfigRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		outcomes := make([]string, len(req.AllowedOutcomes))
		for i, v := range req.AllowedOutcomes {
			if v == nil {
				return nil, 0, errExecutionHTTPBody
			}
			outcomes[i] = *v
		}
		v, err := a.Save(ctx, parent, id, scoring.Config{Name: req.Name, AllowedOutcomes: outcomes, RulesetRef: req.RulesetRef, ConfigJSON: req.ConfigJSON, Active: req.Active})
		status = http.StatusOK
		if op == "scoring.create" {
			status = http.StatusCreated
		}
		return gin.H{"scoring_config": dto.AdaptScoringConfig(v)}, status, err
	default:
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
}
