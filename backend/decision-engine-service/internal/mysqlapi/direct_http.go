package mysqlapi

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h *ExecutionHTTP) registerDirect(v *gin.RouterGroup) {
	v.GET("/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score", h.handle("direct.comparison-scores"))
	v.GET("/tenants/:tenantId/test-runs/:testRunId/data-by-rule-execution", h.handle("direct.comparison-rules"))
	v.POST("/tenants/:tenantId/test-runs/:testRunId/evaluate", h.handle("direct.comparison-evaluate"))
	for _, route := range []struct{ method, path, op string }{
		{"POST", "/tenants/:tenantId/scenarios/:scenarioId/evaluate", "scenario"},
		{"POST", "/tenants/:tenantId/decisions", "create"},
		{"POST", "/tenants/:tenantId/decisions/all", "all"},
		{"POST", "/tenants/:tenantId/ingestion-events/record-ingested", "ingested"},
		{"GET", "/tenants/:tenantId/scenarios/:scenarioId/editor-identifiers", "accessors"},
	} {
		v.Handle(route.method, route.path, h.handle("direct."+route.op))
	}
}

func (h *ExecutionHTTP) direct(c *gin.Context, a *ExecutionEndpointScope, op string) (any, int, error) {
	ctx := c.Request.Context()
	if a.Direct == nil || !a.Direct.MatchesExecutionStore(a.Read.store) {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	if op == "comparison-scores" || op == "comparison-rules" || op == "comparison-evaluate" {
		if a.Administration == nil {
			return nil, 0, mysql.ErrOwnerContractUnavailable
		}
		id := uuid.MustParse(c.Param("testRunId"))
		if op == "comparison-scores" {
			values, err := a.Administration.ComparisonScores(ctx, id)
			return gin.H{"decisions": values}, 200, err
		}
		if op == "comparison-rules" {
			values, err := a.Administration.ComparisonRules(ctx, id)
			return gin.H{"rules": values}, 200, err
		}
		var req dto.EvaluateDecisionRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		result, err := a.Direct.Compare(ctx, a.Administration, id, service.DecisionEvaluationRequest{ObjectID: req.ObjectID, ObjectType: req.ObjectType, Fields: req.Fields})
		if err != nil {
			return nil, 0, err
		}
		return gin.H{"result": dto.AdaptTestRunEvaluation(result)}, 200, nil
	}
	if op == "accessors" {
		if a.Authoring == nil {
			return nil, 0, mysql.ErrOwnerContractUnavailable
		}
		catalog, err := a.Authoring.Accessors(ctx, uuid.MustParse(c.Param("scenarioId")))
		return catalog, http.StatusOK, err
	}
	var parent uuid.UUID
	var req dto.EvaluateDecisionRequest
	if op == "create" {
		var input dto.CreateDecisionRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &input); err != nil {
			return nil, 0, err
		}
		var err error
		parent, err = uuid.Parse(input.ScenarioID)
		if err != nil || parent == uuid.Nil {
			return nil, 0, errExecutionHTTPBody
		}
		req = dto.EvaluateDecisionRequest{ObjectID: input.ObjectID, ObjectType: input.ObjectType, Fields: input.Fields}
	} else if op == "ingested" {
		var input dto.IngestionTriggerRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &input); err != nil {
			return nil, 0, err
		}
		if input.Mode != "" && input.Mode != "sync" && input.Mode != "async" {
			return nil, 0, errExecutionHTTPBody
		}
		if input.Mode == "async" {
			if a.Submission == nil {
				return nil, 0, mysql.ErrOwnerContractUnavailable
			}
			created, err := a.Submission.CreateAsync(ctx, c.Param("tenantId"), service.AsyncDecisionExecutionRequest{ObjectType: input.ObjectType, WaitTimeoutMS: input.WaitTimeoutMS, CallbackURL: input.CallbackURL, Items: []service.DecisionEvaluationRequest{{ObjectID: input.ObjectID, ObjectType: input.ObjectType, Fields: input.Fields}}})
			return gin.H{"deferred": !created.CompletedInline, "async_decision_execution": created.AsyncDecisionExecution}, http.StatusAccepted, err
		}
		if input.CallbackURL != "" || input.WaitTimeoutMS != 0 {
			return nil, 0, errExecutionHTTPBody
		}
		req = dto.EvaluateDecisionRequest{ObjectID: input.ObjectID, ObjectType: input.ObjectType, Fields: input.Fields}
	} else {
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		if op == "scenario" {
			parent = uuid.MustParse(c.Param("scenarioId"))
		}
	}
	result, err := a.Direct.Evaluate(ctx, parent, service.DecisionEvaluationRequest{ObjectID: req.ObjectID, ObjectType: req.ObjectType, Fields: req.Fields})
	if err != nil {
		return nil, 0, err
	}
	if parent != uuid.Nil {
		if len(result.Results) != 1 {
			return nil, 0, mysql.ErrOwnerRelationshipConflict
		}
		return gin.H{"result": dto.AdaptDecisionEvaluation(result.Results[0])}, http.StatusOK, nil
	}
	return gin.H{"result": dto.AdaptMultiDecisionEvaluation(result)}, http.StatusOK, nil
}
