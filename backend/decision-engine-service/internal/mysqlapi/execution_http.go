package mysqlapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlreview"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExecutionScopeFactory MUST derive actor/session authority from trusted request
// context and create action-authorized stores for this tenant. A URL tenant is
// not an identity. There is deliberately no default provider or cmd activation.
type ExecutionScopeFactory func(context.Context, uuid.UUID) (*ExecutionEndpointScope, error)

// Read-only/admin endpoints need no evaluation/model provider. Submission is
// mandatory for creation, and must share the exact authorized read store.
type ExecutionEndpointScope struct {
	Read           *ExecutionAdapter
	Review         *mysqlreview.Store
	Submission     *ExecutionSubmission
	Direct         *mysqlexecution.Direct
	Effects        *mysql.EffectAdministration
	Schedules      *mysqlexecution.ScheduleAdmin
	Publication    *mysqlexecution.Publication
	Authoring      *mysqlexecution.Authoring
	Scoring        *mysqlexecution.ScoringAuthoring
	History        *mysql.DecisionStore
	Administration *mysql.AdministrationStore
	CustomLists    *mysql.CustomListAdmin
	Platform       *mysql.PlatformAdmin
	Browser        *mysql.BrowserWorkflowAdmin
	Clock          ports.Clock
}

type ExecutionHTTP struct {
	scope    ExecutionScopeFactory
	maxBytes int64
}

func NewExecutionHTTP(scope ExecutionScopeFactory, maxBytes int64) (*ExecutionHTTP, error) {
	if scope == nil || maxBytes < 1024 || maxBytes > 64<<20 {
		return nil, errors.New("trusted request scope and HTTP body budget [1KiB,64MiB] required")
	}
	return &ExecutionHTTP{scope, maxBytes}, nil
}

// Register is explicitly opt-in. It must replace, not duplicate, the source
// execution routes only after backend/auth/provider readiness is approved.
func (h *ExecutionHTTP) Register(v1 *gin.RouterGroup) {
	h.registerHistory(v1)
	h.registerAdministration(v1)
	h.registerCustomLists(v1)
	h.registerPlatform(v1)
	h.registerAuthoring(v1)
	h.registerScoring(v1)
	h.registerDirect(v1)
	h.registerWorkflowRules(v1)
	h.registerScreeningConfigs(v1)
	h.registerEffectAdministration(v1)
	h.registerReview(v1)
	h.registerExpressionAssistance(v1)
	// The containing application group authenticates this tenant-independent
	// evaluator catalog. It requires no fabricated tenant or database authority.
	v1.GET("/rule-functions", func(c *gin.Context) {
		if c.Request.URL.RawQuery != "" {
			executionHTTPError(c, mysql.ErrExecutionPage)
			return
		}
		c.JSON(http.StatusOK, gin.H{"rule_functions": dto.AdaptRuleFunctionCatalog(ast_eval.SupportedFunctionCatalog())})
	})
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/deactivate", h.handle("publication.deactivate"))
	scheduled := "/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions"
	async := "/tenants/:tenantId/async-decision-executions"
	v1.POST(scheduled, h.handle("scheduled.create"))
	v1.GET(scheduled, h.handle("scheduled.list"))
	v1.GET(scheduled+"/status-summary", h.handle("scheduled.summary"))
	v1.GET(scheduled+"/:executionId", h.handle("scheduled.get"))
	v1.POST(scheduled+"/:executionId/cancel", h.handle("scheduled.cancel"))
	v1.POST(scheduled+"/:executionId/retry", h.handle("scheduled.redrive"))
	v1.POST(async, h.handle("async.create"))
	v1.GET(async, h.handle("async.list"))
	v1.GET(async+"/status-summary", h.handle("async.summary"))
	v1.GET(async+"/:executionId", h.handle("async.get"))
	v1.POST(async+"/:executionId/cancel", h.handle("async.cancel"))
	v1.POST(async+"/:executionId/retry", h.handle("async.redrive"))
	v1.POST(async+"/:executionId/callback/cancel", h.handle("callback.cancel"))
	v1.POST(async+"/:executionId/callback/retry", h.handle("callback.redrive"))
	schedule := "/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule"
	v1.GET(schedule, h.handle("schedule.get"))
	v1.GET(schedule+"/:scheduleId", h.handle("schedule.get"))
	v1.PUT(schedule, h.handle("schedule.replace"))
	v1.DELETE(schedule, h.handle("schedule.disable"))
	publication := "/tenants/:tenantId/scenarios/:scenarioId/publications"
	v1.GET(publication, h.handle("publication.list"))
	v1.POST(publication, h.handle("publication.action"))
	v1.GET(publication+"/preparation", h.handle("publication.preparation.get"))
	v1.POST(publication+"/preparation", h.handle("publication.preparation.start"))
}

var errExecutionHTTPBody = errors.New("invalid execution request JSON")
var errExecutionHTTPSize = errors.New("execution request exceeds HTTP budget")

// Decode twice: a bounded token walk rejects duplicate keys/depth/node abuse,
// then typed decoding rejects unknown fields without losing JSON numbers.
func decodeExecutionHTTP(body io.Reader, max int64, out any) error {
	if body == nil {
		return errExecutionHTTPBody
	}
	raw, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return errExecutionHTTPBody
	}
	if int64(len(raw)) > max {
		return errExecutionHTTPSize
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var visit func(int) error
	visit = func(depth int) error {
		nodes++
		if depth > 64 || nodes > 100000 {
			return errExecutionHTTPSize
		}
		t, err := d.Token()
		if err != nil {
			return errExecutionHTTPBody
		}
		switch t {
		case json.Delim('{'):
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return errExecutionHTTPBody
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return errExecutionHTTPBody
				}
				keys[name] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return errExecutionHTTPBody
			}
		case json.Delim('['):
			for d.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return errExecutionHTTPBody
			}
		default:
			if _, ok := t.(json.Delim); ok {
				return errExecutionHTTPBody
			}
		}
		return nil
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return errExecutionHTTPBody
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errExecutionHTTPBody
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errExecutionHTTPBody
	}
	return nil
}

func (h *ExecutionHTTP) handle(op string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		tenant, err := uuid.Parse(c.Param("tenantId"))
		if err != nil || tenant == uuid.Nil {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		for _, name := range []string{"scenarioId", "executionId", "scheduleId", "iterationId", "ruleId", "configId", "decisionId", "testRunId", "requestId", "workflowId", "listId", "entryId", "conditionId", "actionId", "caseId"} {
			if value := c.Param(name); value != "" {
				id, err := uuid.Parse(value)
				if err != nil || id == uuid.Nil {
					executionHTTPError(c, mysql.ErrTenantScope)
					return
				}
			}
		}
		scopeStarted := time.Now()
		api, err := h.scope(ctx, tenant)
		ports.RecordEvaluationDuration(ctx, "scope", time.Since(scopeStarted))
		if err != nil {
			executionHTTPError(c, err)
			return
		}
		if api == nil || api.Read == nil || api.Read.store == nil || api.Submission != nil && (api.Submission.read == nil || api.Submission.read.store != api.Read.store || api.Submission.admission == nil || !api.Submission.admission.MatchesExecutionStore(api.Read.store)) {
			executionHTTPError(c, mysql.ErrDecisionAuthorizationUnavailable)
			return
		}
		// Enforce the path scope even if a faulty factory returns another tenant.
		bound, rowBudget, _, err := api.Read.store.ExecutionPreparationScope()
		if err != nil || bound != tenant || api.Schedules != nil && !api.Schedules.MatchesExecutionStore(api.Read.store) || api.Publication != nil && !api.Publication.MatchesExecutionStore(api.Read.store) || api.Authoring != nil && !api.Authoring.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		if api.Scoring != nil && !api.Scoring.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		var out any
		if strings.HasPrefix(op, "assist.") {
			value, err := h.expressionAssistance(c, api, op)
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			c.JSON(200, value)
			return
		}
		if strings.HasPrefix(op, "review.") {
			h.review(c, api, op)
			return
		}
		if api.Browser != nil && !api.Browser.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		if api.Platform != nil && !api.Platform.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		if api.CustomLists != nil && !api.CustomLists.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		if api.Administration != nil && !api.Administration.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		if api.History != nil && !api.History.MatchesExecutionStore(api.Read.store) {
			executionHTTPError(c, mysql.ErrTenantScope)
			return
		}
		if len(c.Request.URL.RawQuery) > 4096 {
			executionHTTPError(c, errExecutionHTTPSize)
			return
		}
		query, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
		if queryErr != nil || len(query) > 0 && op != "scheduled.list" && op != "async.list" && op != "publication.list" && op != "publication.preparation.get" && !strings.HasPrefix(op, "history.") && !strings.HasPrefix(op, "platform.") {
			executionHTTPError(c, mysql.ErrExecutionPage)
			return
		}
		status := http.StatusOK
		if strings.HasPrefix(op, "effect.") {
			if api.Effects == nil || !api.Effects.MatchesExecutionStore(api.Read.store) {
				executionHTTPError(c, mysql.ErrOwnerContractUnavailable)
				return
			}
			out, err = h.effectAdministration(c, api.Effects, strings.TrimPrefix(op, "effect."))
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			c.JSON(200, out)
			return
		}
		if strings.HasPrefix(op, "screening-config.") {
			out, status, err = h.screeningConfig(c, api.Administration, strings.TrimPrefix(op, "screening-config."))
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			if status == 204 {
				c.Status(status)
			} else {
				c.JSON(status, out)
			}
			return
		}
		if strings.HasPrefix(op, "workflow-rules.") {
			out, status, err = h.workflowRules(c, api, strings.TrimPrefix(op, "workflow-rules."))
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			if status == 204 {
				c.Status(status)
			} else {
				c.JSON(status, out)
			}
			return
		}
		if strings.HasPrefix(op, "direct.") {
			out, status, err = h.direct(c, api, strings.TrimPrefix(op, "direct."))
			if timing := evaluationServerTiming(ctx); timing != "" {
				c.Header("Server-Timing", timing)
			}
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			c.JSON(status, out)
			return
		}
		if strings.HasPrefix(op, "browser-screening.") {
			h.browserScreening(c, api.Browser, op)
			return
		}
		if strings.HasPrefix(op, "browser.") {
			out, err = h.browserWorkflow(c, api.Browser, strings.TrimPrefix(op, "browser."))
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			c.JSON(status, out)
			return
		}
		if strings.HasPrefix(op, "platform.") {
			out, status, err = h.platform(c, api, strings.TrimPrefix(op, "platform."), query)
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			c.JSON(status, out)
			return
		}
		if strings.HasPrefix(op, "lists.") {
			out, status, err = h.customLists(c, api.CustomLists, strings.TrimPrefix(op, "lists."), rowBudget)
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			if status == http.StatusNoContent {
				c.Status(status)
			} else {
				c.JSON(status, out)
			}
			return
		}
		if strings.HasPrefix(op, "admin.") {
			out, status, err = h.administration(c, api.Administration, strings.TrimPrefix(op, "admin."))
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			if status == http.StatusNoContent {
				c.Status(status)
			} else {
				c.JSON(status, out)
			}
			return
		}
		if strings.HasPrefix(op, "history.") {
			out, err = h.history(c, api, strings.TrimPrefix(op, "history."), query, rowBudget)
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			c.JSON(status, out)
			return
		}
		if strings.HasPrefix(op, "scoring.") {
			out, status, err = h.scoring(c, api.Scoring, op)
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			if status == http.StatusNoContent {
				c.Status(status)
			} else {
				c.JSON(status, out)
			}
			return
		}
		if strings.HasPrefix(op, "authoring.") {
			out, status, err = h.authoring(c, api.Authoring, strings.TrimPrefix(op, "authoring."))
			if err != nil {
				executionHTTPError(c, err)
				return
			}
			if status == http.StatusNoContent {
				c.Status(status)
			} else {
				c.JSON(status, out)
			}
			return
		}
		scenario, id := c.Param("scenarioId"), c.Param("executionId")
		if (op == "scheduled.create" || op == "async.create") && api.Submission == nil {
			executionHTTPError(c, mysql.ErrOwnerContractUnavailable)
			return
		}
		switch op {
		case "publication.preparation.get", "publication.preparation.start":
			out, err = h.publicationPreparation(c, api, uuid.MustParse(scenario), op == "publication.preparation.start")
			if op == "publication.preparation.start" {
				status = http.StatusAccepted
			}
		case "publication.deactivate":
			out, err = h.deactivate(c, api)
		case "publication.list", "publication.action":
			out, err = h.publication(c, api, tenant, uuid.MustParse(scenario), op == "publication.list")
		case "schedule.get":
			if len(c.Request.URL.Query()) != 0 {
				err = mysql.ErrExecutionPage
				break
			}
			parent := uuid.MustParse(scenario)
			scheduleID := uuid.Nil
			if c.Param("scheduleId") != "" {
				scheduleID = uuid.MustParse(c.Param("scheduleId"))
			}
			var result *ScheduleResponse
			result, err = api.Read.Schedule(ctx, parent, scheduleID)
			out = gin.H{"recurring_schedule": result}
		case "schedule.replace":
			if api.Schedules == nil {
				err = mysql.ErrOwnerContractUnavailable
				break
			}
			var req mysqlexecution.CalendarScheduleRequest
			err = decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req)
			if err == nil {
				var result mysql.ExecutionScheduleRecord
				result, err = api.Schedules.Replace(ctx, uuid.MustParse(scenario), req)
				if err == nil {
					out = gin.H{"recurring_schedule": adaptSchedule(result)}
				}
			}
		case "schedule.disable":
			var req DisableScheduleRequest
			err = decodeExecutionHTTP(c.Request.Body, 1024, &req)
			if err == nil {
				var result ScheduleResponse
				result, err = api.Read.DisableSchedule(ctx, uuid.MustParse(scenario), req)
				out = gin.H{"recurring_schedule": result}
			}
		case "scheduled.list", "async.list":
			var f mysql.ExecutionPageFilter
			f, err = executionPageFilter(query)
			if _, explicit := query["limit"]; !explicit {
				f.Limit = min(f.Limit, rowBudget)
			}
			if err == nil {
				out, err = api.Read.Page(ctx, tenant.String(), scenario, f)
			}
		case "scheduled.summary", "async.summary":
			if len(c.Request.URL.Query()) != 0 {
				err = mysql.ErrExecutionPage
			} else {
				out, err = api.Read.StatusSummary(ctx, tenant.String(), scenario)
			}
		case "scheduled.create":
			var req dto.CreateScheduledExecutionRequest
			err = decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req)
			if err == nil {
				out, err = api.Submission.CreateScheduled(ctx, tenant.String(), scenario, req.ScheduledFor, dto.AdaptScheduledExecutionRequest(req))
				status = http.StatusAccepted
			}
		case "async.create":
			var req dto.CreateAsyncDecisionExecutionRequest
			err = decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req)
			if err == nil {
				var result AsyncCreationResponse
				result, err = api.Submission.CreateAsync(ctx, tenant.String(), dto.AdaptAsyncExecutionRequest(req))
				out = result
				if !result.CompletedInline {
					status = http.StatusAccepted
				}
			}
		case "scheduled.get":
			out, err = api.Read.GetScheduled(ctx, tenant.String(), scenario, id)
		case "async.get":
			out, err = api.Read.GetAsync(ctx, tenant.String(), id)
		case "scheduled.cancel", "async.cancel", "callback.cancel":
			var req CancellationRequest
			err = decodeExecutionHTTP(c.Request.Body, 1024, &req)
			if err == nil {
				err = req.Validate()
			}
			if err == nil {
				switch op {
				case "scheduled.cancel":
					out, err = api.Read.CancelScheduled(ctx, tenant.String(), scenario, id, req)
				case "async.cancel":
					out, err = api.Read.CancelAsync(ctx, tenant.String(), id, req)
				case "callback.cancel":
					out, err = api.Read.CancelCallback(ctx, tenant.String(), id, req)
				}
			}
		case "scheduled.redrive", "async.redrive", "callback.redrive":
			var req RedriveRequest
			err = decodeExecutionHTTP(c.Request.Body, 1024, &req)
			if err == nil {
				err = req.Validate()
			}
			if err == nil {
				switch op {
				case "scheduled.redrive":
					out, err = api.Read.RedriveScheduled(ctx, tenant.String(), scenario, id, req)
				case "async.redrive":
					out, err = api.Read.RedriveAsync(ctx, tenant.String(), id, req)
				case "callback.redrive":
					out, err = api.Read.RedriveCallback(ctx, tenant.String(), id, req)
				}
			}
		default:
			err = mysql.ErrDecisionAuthorizationUnavailable
		}
		if err != nil {
			executionHTTPError(c, err)
			return
		}
		c.JSON(status, out)
	}
}

func executionHTTPError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "execution_failed"
	var uncertain *mysqlexecution.AdmissionOutcomeUnknown
	var wait *ExecutionWaitError
	var commit *mysqlstore.CommitError
	var rollback *mysqlstore.RollbackError
	var executionID string
	var invalid *payload.Error
	var modelContract *payload.ModelContractError
	switch {
	case errors.Is(err, screening.ErrInlineUnavailable), errors.Is(err, screening.ErrInlineFailed):
		status, code = 503, "screening_evaluation_unavailable"
	case errors.Is(err, screening.ErrInvalidQuery):
		status, code = 422, "invalid_screening_input"
	case errors.As(err, &uncertain):
		status, code, executionID = 503, "admission_outcome_unknown", uncertain.ExecutionID
	case errors.As(err, &wait):
		status, code, executionID = 503, "execution_admitted_wait_failed", wait.ExecutionID
	case errors.As(err, &commit), errors.As(err, &rollback):
		status, code = 503, "mutation_outcome_unknown"
	case errors.Is(err, mysql.ErrDecisionAuthorizationDenied), errors.Is(err, workflow.ErrAuthoringDenied):
		status, code = 403, "forbidden"
	case errors.Is(err, mysql.ErrDecisionAuthorizationUnavailable), errors.Is(err, mysql.ErrOwnerContractUnavailable), errors.Is(err, mysql.ErrAdditionalScoringDeferred):
		status, code = 503, "required_provider_unavailable"
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "execution_not_found"
	case errors.Is(err, mysql.ErrExecutionState), errors.Is(err, mysql.ErrEffectRetryState), errors.Is(err, mysql.ErrEffectSourceOwned), errors.Is(err, mysql.ErrExecutionReplayConflict), errors.Is(err, mysql.ErrExecutionBindingChanged), errors.Is(err, mysql.ErrOwnerRelationshipConflict), errors.Is(err, mysql.ErrPublicationState), errors.Is(err, mysql.ErrCommittedIteration), errors.Is(err, workflow.ErrAuthoringConflict):
		status, code = 409, "execution_conflict"
	case errors.Is(err, errExecutionHTTPSize), errors.Is(err, mysql.ErrProjectionLimit), errors.Is(err, service.ErrExecutionPreparationLimit):
		status, code = 413, "execution_budget_exceeded"
	case errors.Is(err, errExecutionHTTPBody), errors.Is(err, mysql.ErrExecutionPage), errors.Is(err, ErrCancellationRequest), errors.Is(err, ErrRedriveRequest), errors.Is(err, mysql.ErrTenantScope):
		status, code = 400, "invalid_execution_request"
	case errors.Is(err, service.ErrExecutionPreparation), errors.Is(err, mysql.ErrAggregateCapability), errors.Is(err, mysql.ErrAdministrationInput):
		status, code = 422, "invalid_execution_input"
	case errors.As(err, &modelContract):
		status, code = 503, "published_model_invalid"
	case errors.As(err, &invalid):
		status, code = 422, "invalid_execution_input"
		if invalid.Category == "stored_record_invalid" {
			status, code = 500, "stored_record_invalid"
		}
	case errors.Is(err, mysql.ErrPublishedModelUnavailable):
		status, code = 409, "published_model_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = 504, "execution_deadline_exceeded"
	case errors.Is(err, context.Canceled):
		status, code = 408, "execution_request_cancelled"
	}
	body := gin.H{"error": code}
	if status >= 500 {
		method := ""
		if c.Request != nil {
			method = c.Request.Method
		}
		slog.Error("decision API operation failed", "method", method, "route", c.FullPath(), "error", err)
	}
	if id, parseErr := uuid.Parse(executionID); parseErr == nil && id != uuid.Nil {
		body["execution_id"] = id.String()
	}
	c.AbortWithStatusJSON(status, body)
}
