package mysqlapi

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"strings"
)

func (h *ExecutionHTTP) registerEffectAdministration(v *gin.RouterGroup) {
	for _, r := range []struct{ method, path, op string }{
		{"GET", "/tenants/:tenantId/decisions/:decisionId/screening-executions", "screenings"},
		{"GET", "/tenants/:tenantId/screening-executions/:executionId", "screening"},
		{"POST", "/tenants/:tenantId/screening-executions/:executionId/status", "screening.status"},
		{"POST", "/tenants/:tenantId/screening-executions/:executionId/retry", "screening.retry"},
		{"POST", "/tenants/:tenantId/scoring-requests/:requestId/status", "scoring.status"},
		{"POST", "/tenants/:tenantId/scoring-requests/:requestId/retry", "scoring.retry"},
	} {
		v.Handle(r.method, r.path, h.handle("effect."+r.op))
	}
}
func (h *ExecutionHTTP) effectAdministration(c *gin.Context, s *mysql.EffectAdministration, op string) (any, error) {
	ctx := c.Request.Context()
	if op == "screenings" {
		values, err := s.Screenings(ctx, uuid.MustParse(c.Param("decisionId")))
		return gin.H{"screening_executions": adaptHistory(values, dto.AdaptScreeningExecution)}, err
	}
	kind := ports.JobScreening
	raw := c.Param("executionId")
	if strings.HasPrefix(op, "scoring.") {
		kind = ports.JobScoring
		raw = c.Param("requestId")
	}
	id := uuid.MustParse(raw)
	if op == "screening" {
		value, err := s.Screening(ctx, id)
		return gin.H{"screening_execution": dto.AdaptScreeningExecution(value)}, err
	}
	if strings.HasSuffix(op, ".retry") {
		if err := h.emptyAuthoringBody(c); err != nil {
			return nil, err
		}
		if err := s.Retry(ctx, id, kind); err != nil {
			return nil, err
		}
		if kind == ports.JobScreening {
			value, err := s.Screening(ctx, id)
			return gin.H{"screening_execution": dto.AdaptScreeningExecution(value)}, err
		}
		value, err := s.Scoring(ctx, id)
		return gin.H{"scoring_request": dto.AdaptScoringRequest(value)}, err
	}
	var input struct {
		Status            string           `json:"status"`
		ProviderReference *string          `json:"provider_reference"`
		ResponseJSON      *json.RawMessage `json:"response_json"`
		LastError         *string          `json:"last_error"`
	}
	if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &input); err != nil {
		return nil, err
	}
	value, err := s.UpdateStatus(ctx, id, kind, mysql.EffectStatusInput{Status: input.Status, ProviderReference: input.ProviderReference, ResponseJSON: input.ResponseJSON, LastError: input.LastError})
	if err != nil {
		return nil, err
	}
	if kind == ports.JobScreening {
		return gin.H{"screening_execution": dto.AdaptScreeningExecution(value.(screening.Execution))}, nil
	}
	return gin.H{"scoring_request": dto.AdaptScoringRequest(value.(scoring.Request))}, nil
}

// The application must authenticate this group before registration. Callback
// JSON is only a reconciliation hint; source SQL owns the actual result.
func (h *ExecutionHTTP) RegisterInternal(group *gin.RouterGroup) {
	group.POST("/internal/screening-status-updates", func(c *gin.Context) {
		var req struct {
			TenantID    string  `json:"tenant_id"`
			ScreeningID string  `json:"screening_id"`
			DecisionID  string  `json:"decision_id"`
			ScenarioID  string  `json:"scenario_id"`
			ConfigID    string  `json:"screening_config_id"`
			Status      string  `json:"status"`
			Provider    string  `json:"provider"`
			ObjectType  string  `json:"object_type"`
			ObjectID    string  `json:"object_id"`
			Reference   string  `json:"provider_reference"`
			LastError   string  `json:"last_error"`
			Partial     bool    `json:"partial"`
			Key         string  `json:"idempotency_key"`
			CompletedAt *string `json:"completed_at"`
			MatchCount  int     `json:"match_count"`
		}
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			executionHTTPError(c, err)
			return
		}
		ids := make([]uuid.UUID, 4)
		for i, raw := range []string{req.TenantID, req.ScreeningID, req.DecisionID, req.ConfigID} {
			id, err := uuid.Parse(raw)
			if err != nil || id == uuid.Nil {
				executionHTTPError(c, errExecutionHTTPBody)
				return
			}
			ids[i] = id
		}
		api, err := h.scope(c.Request.Context(), ids[0])
		if err != nil {
			executionHTTPError(c, err)
			return
		}
		if api == nil || api.Effects == nil || api.Read == nil || !api.Effects.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrOwnerContractUnavailable)
			return
		}
		scenario := uuid.Nil
		if req.ScenarioID != "" {
			scenario, err = uuid.Parse(req.ScenarioID)
			if err != nil || scenario == uuid.Nil {
				executionHTTPError(c, errExecutionHTTPBody)
				return
			}
		}
		value, err := api.Effects.ReconcileScreening(c.Request.Context(), ids[0], ids[1], ids[2], ids[3], scenario)
		if err != nil {
			executionHTTPError(c, err)
			return
		}
		c.JSON(200, gin.H{"screening_execution": dto.AdaptScreeningExecution(value)})
	})
}
