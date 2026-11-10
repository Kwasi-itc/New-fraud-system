package mysqlruntime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlreview"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localdelivery"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localtenants"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const LocalAPIUser = "local_decision_api"
const LocalWorkerUser = "local_decision_worker"
const LocalProviderUser = "local_decision_provider"

type LocalConfig struct {
	TenantMode           string
	ReviewLegacyTenantID uuid.UUID
	TenantID             uuid.UUID
	ReviewerID           uuid.UUID
	ReviewInboxID        uuid.UUID
	AccountHost          string
	Plan                 migrations.Plan
	DeliveryPlan         migrations.Plan
}

func LocalSecret(name string) (string, error) {
	switch name {
	case "decision_api_password", "decision_worker_password", "decision_runtime_token", "decision_review_session", "decision_provider_password", "decision_provider_token":
	default:
		return "", errors.New("unknown decision local secret")
	}
	data, err := os.ReadFile("/run/secrets/" + name)
	if err != nil {
		return "", err
	}
	value, err := hex.DecodeString(string(data))
	if err != nil || len(value) != 32 {
		return "", errors.New("invalid generated decision secret")
	}
	return string(data), nil
}

type localIDs struct{}

func (localIDs) New() uuid.UUID { return uuid.New() }

type localClock struct{}

func (localClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type localCredential struct{}
type localReviewCredential struct{}

func LocalReviewCredential() (string, error) {
	raw, err := LocalSecret("decision_review_session")
	if err != nil {
		return "", err
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(decoded), nil
}

// This operator-only CLI helper creates a genuine case through its SQL owner.
func (a *Application) CreateLocalReviewFixture(ctx context.Context, decision uuid.UUID) (uuid.UUID, error) {
	return a.CreateLocalReviewFixtureForTenant(ctx, a.tenants[0], decision)
}
func (a *Application) CreateLocalReviewFixtureForTenant(ctx context.Context, tenant, decision uuid.UUID) (uuid.UUID, error) {
	credential, err := LocalTenantReviewCredential(tenant, a.localReviewLegacyTenantID)
	if err != nil {
		return uuid.Nil, err
	}
	v, err := mysqlidentity.NewVerifier(a.runtime.deps.DB, a.runtime.deps.Database)
	if err != nil {
		return uuid.Nil, err
	}
	session, err := v.Resolve(ctx, credential)
	if err != nil {
		return uuid.Nil, err
	}
	owner, err := mysqlreview.New(a.runtime.deps.DB, a.runtime.deps.Database, session)
	if err != nil {
		return uuid.Nil, err
	}
	return owner.CreateCaseForDecision(ctx, decision)
}

type localModels struct{ *datamodel.HTTPClient }

// The dynamic development host retains no unbounded per-tenant model cache.
func (m localModels) GetTenantModel(ctx context.Context, tenant string) (ports.TenantModel, error) {
	return m.HTTPClient.GetFreshTenantModel(ctx, tenant)
}

// Each tenant gets a distinct opaque session. Preserve the original credential
// for the bootstrap fixture tenant so existing review history remains usable.
func LocalTenantReviewCredential(tenant, fixtureTenant uuid.UUID) (string, error) {
	raw, err := LocalSecret("decision_review_session")
	if err != nil {
		return "", err
	}
	return localTenantReviewCredential(raw, tenant, fixtureTenant)
}
func localTenantReviewCredential(raw string, tenant, fixtureTenant uuid.UUID) (string, error) {
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != 32 || tenant == uuid.Nil {
		return "", errors.New("valid tenant and review secret required")
	}
	if tenant != fixtureTenant {
		mac := hmac.New(sha256.New, decoded)
		mac.Write([]byte("local-decision-review/v1/" + tenant.String()))
		decoded = mac.Sum(nil)
	}
	return base64.RawURLEncoding.EncodeToString(decoded), nil
}

func (localModels) PublishedRecordKeyContract() string { return "data_model_v005" }

func localPolicy() Policy {
	p := Policy{MaxRows: 500, MaxBytes: 4 << 20, HTTPMaxBytes: 1 << 20, MaxWorkerTenants: 64, MaxWorkerSlots: 16,
		Lease: time.Minute, StartupTimeout: 30 * time.Second, ReadinessTimeout: 3 * time.Second, RequestTimeout: 15 * time.Second,
		VelocitySummaries: true,
		ReadHeaderTimeout: 5 * time.Second, HTTPIdleTimeout: time.Minute, HTTPMaxHeaderBytes: 16 << 10,
		MaintenancePageSize: 20, MaintenancePagesPerTick: 20, MaintenanceInterval: 5 * time.Second, MaintenanceTimeout: 3 * time.Second,
		Evaluation: mysqlexecution.EvaluationPolicy{Limits: service.ExecutionPreparationLimits{MaxItems: 100, MaxBytes: 4 << 20}, Aggregates: []string{"count", "count_distinct", "sum", "avg", "min", "max"}, PublishOutboxEvents: true, RuleConcurrency: 4, AggregateConcurrency: 4, AggregateTimeout: 2 * time.Second, AggregateBatchWindow: 150 * time.Microsecond},
		Admission:  mysqlexecution.AdmissionPolicy{ScheduledMaxAttempts: 3, AsyncMaxAttempts: 3, CallbackAllowedURL: localdelivery.CallbackURL},
		Execution:  store.ExecutionPolicy{Queues: map[ports.JobKind]store.DeliveryQueuePolicy{}, CallbackMaxAttempts: 3},
		Effects:    map[ports.JobKind]store.EffectQueuePolicy{}, Workers: map[ports.JobKind]mysqlexecution.EvaluationWorkerPolicy{},
		Wait:      mysqlapi.ExecutionWaitPolicy{MaxWindow: time.Second, PollInterval: 100 * time.Millisecond},
		Scheduler: mysqlexecution.SchedulerPolicy{PageSize: 10, OccurrencesPerSchedule: 5, Interval: 100 * time.Millisecond, TickTimeout: 3 * time.Second}}
	for _, kind := range allKinds {
		q := store.DeliveryQueuePolicy{Name: "local_decision_" + string(kind), MaxAttempts: 3}
		if kind == ports.JobScheduled || kind == ports.JobAsync || kind == ports.JobCallback {
			p.Execution.Queues[kind] = q
		} else {
			p.Effects[kind] = q
		}
		p.Workers[kind] = mysqlexecution.EvaluationWorkerPolicy{Concurrency: 1, PollInterval: time.Second, PollJitter: 100 * time.Millisecond, ClaimTimeout: 3 * time.Second, HeartbeatInterval: 10 * time.Second, HeartbeatTimeout: 3 * time.Second, WorkTimeout: 30 * time.Second, DomainRetryBackoff: time.Second}
	}
	return p
}

// OpenLocal is an explicitly opted-in development host. It installs real,
// bounded local receipt clients without weakening Application.Start's foreign
// provider gates. Private local receivers persist effects through their owners.
func OpenLocal(ctx context.Context, worker bool) (*Application, *sql.DB, error) {
	_, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN"))
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile("/runtime/config.json")
	if err != nil {
		return nil, nil, err
	}
	var c LocalConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, nil, err
	}
	if c.TenantMode != "all-active" || c.TenantID == uuid.Nil || c.Plan.Database != "fraud" || c.Plan.Owner != "decision_engine" {
		return nil, nil, errors.New("explicit shared local decision scope required")
	}
	if c.ReviewLegacyTenantID == uuid.Nil {
		c.ReviewLegacyTenantID = c.TenantID
	}
	if _, err := metadata.DispatchPlan("fraud", "local_decision_guard", c.Plan.Steps); err != nil {
		return nil, nil, err
	}
	user, secret := LocalAPIUser, "decision_api_password"
	if worker {
		user, secret = LocalWorkerUser, "decision_worker_password"
	}
	password, err := LocalSecret(secret)
	if err != nil {
		return nil, nil, err
	}
	token, err := LocalSecret("decision_runtime_token")
	if err != nil {
		return nil, nil, err
	}
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: "fraud", User: user, Password: password, AllowInsecureTransport: true, InterpolateParameters: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 5 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*Application, *sql.DB, error) { db.Close(); return nil, nil, err }
	models := localModels{datamodel.NewHTTPClient("http://data-model-service:8080", 5*time.Second)}
	registry, err := localtenants.New(db, "fraud")
	if err != nil {
		return fail(err)
	}
	frontendIdentity, err := localFrontendIdentity(db)
	if err != nil {
		return fail(err)
	}
	authority := func(serviceScope bool) AuthorityProvider {
		return func(ctx context.Context, tenant uuid.UUID) (Authorities, error) {
			if !serviceScope && ctx.Value(verifiedSessionKey{}) != nil {
				return frontendIdentity.RequestAuthority(ctx, tenant)
			}
			if tenant == uuid.Nil || !serviceScope && ctx.Value(localCredential{}) != token {
				return Authorities{}, store.ErrDecisionAuthorizationDenied
			}
			checkTenant := func(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
				if id != tenant {
					return store.ErrDecisionAuthorizationDenied
				}
				err := registry.Authorize(ctx, tx, id)
				if err == localtenants.ErrDenied {
					return store.ErrDecisionAuthorizationDenied
				}
				return err
			}
			platform := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ bool) error { return checkTenant(ctx, tx, id) }
			reads := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, write bool) error {
				if write {
					return store.ErrDecisionAuthorizationDenied
				}
				return checkTenant(ctx, tx, id)
			}
			var review *mysqlidentity.Session
			if !serviceScope && ctx.Value(localReviewCredential{}) == true {
				credential, err := LocalTenantReviewCredential(tenant, c.ReviewLegacyTenantID)
				if err != nil {
					return Authorities{}, err
				}
				verifier, err := mysqlidentity.NewVerifier(db, "fraud")
				if err != nil {
					return Authorities{}, err
				}
				session, err := verifier.Resolve(ctx, credential)
				if err != nil {
					return Authorities{}, store.ErrDecisionAuthorizationDenied
				}
				if session.TenantID != tenant {
					return Authorities{}, store.ErrDecisionAuthorizationDenied
				}
				review = &session
			}
			var trusted *store.TrustedDecisionSession
			if review != nil {
				trusted = &store.TrustedDecisionSession{ActorID: review.ActorID, TenantID: review.TenantID, SessionID: review.SessionID, MembershipVersion: review.MembershipVersion, ReadOnly: review.ReadOnly}
			}
			return Authorities{TenantID: tenant, ReviewSession: review, Session: trusted, Authoring: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ store.AuthoringAction) error {
				return checkTenant(ctx, tx, id)
			}, Reads: reads, Persist: platform, PlatformMutations: platform, EffectMutations: platform, EffectRecovery: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ ports.JobKind, _ store.EffectRetryAction) error {
				return checkTenant(ctx, tx, id)
			}, Executions: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ store.ExecutionAction) error {
				return checkTenant(ctx, tx, id)
			}, Worker: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ ports.JobKind, _ store.EffectWorkerAction) error {
				return checkTenant(ctx, tx, id)
			}}, nil
		}
	}
	p := localPolicy()
	providerToken, err := LocalSecret("decision_provider_token")
	if err != nil {
		return fail(err)
	}
	dispatch := map[ports.JobKind]ports.ReceiptDispatcher{}
	for _, kind := range dispatchKinds {
		client, err := localdelivery.NewMultiTenantClient(localdelivery.BaseURL, providerToken, kind)
		if err != nil {
			return fail(err)
		}
		dispatch[kind] = client
	}
	r := &Runtime{deps: Dependencies{DB: db, Database: "fraud", Models: models, IndexPreparation: models, RequestAuthority: authority(false), ServiceAuthority: authority(true), IDs: localIDs{}, Clock: localClock{}, Dispatch: dispatch}, policy: p}
	runner, err := migrations.New(db, c.Plan)
	if err != nil {
		return fail(err)
	}
	check := func(ctx context.Context) error {
		var version, login, role string
		if err := db.QueryRowContext(ctx, "SELECT VERSION(),CURRENT_USER(),CURRENT_ROLE()").Scan(&version, &login, &role); err != nil {
			return err
		}
		if version != "8.4.8" || !strings.HasPrefix(login, user+"@") || role != "NONE" {
			return errors.New("pinned MySQL and restricted local account required")
		}
		history, err := runner.Status(ctx)
		if err != nil {
			return err
		}
		if len(history) != len(c.Plan.Steps) {
			return migrations.ErrLineage
		}
		if c.DeliveryPlan.Owner != localdelivery.Owner || c.DeliveryPlan.Database != "fraud" {
			return errors.New("local delivery migration plan required")
		}
		deliveryRunner, err := migrations.NewForOwner(db, localdelivery.Owner, c.DeliveryPlan)
		if err != nil {
			return err
		}
		deliveryHistory, err := deliveryRunner.Status(ctx)
		if err != nil {
			return err
		}
		if len(deliveryHistory) != len(c.DeliveryPlan.Steps) {
			return migrations.ErrLineage
		}
		for _, row := range deliveryHistory {
			if row.State != "applied" {
				return migrations.ErrLineage
			}
		}
		for _, h := range history {
			if h.State != "applied" {
				return migrations.ErrLineage
			}
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return fail(err)
	}
	loops, err := r.localWorkerLoops(registry)
	if err != nil {
		return fail(err)
	}
	a := &Application{runtime: r, tenants: []uuid.UUID{c.TenantID}, workers: loops, localReviewLegacyTenantID: c.ReviewLegacyTenantID}
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	engine.GET("/readyz", func(c *gin.Context) {
		bounded, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		if !a.ready.Load() || a.stopped.Load() || worker && !a.workerRunning.Load() {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		if err := check(bounded); err != nil {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready", "database": "mysql", "profile": "local-development", "tenant_mode": "all-active", "default_tenant_id": a.tenants[0]})
	})
	if !worker {
		httpapi.RegisterLocalDocs(engine)
		engine.GET("/capabilities", func(c *gin.Context) {
			c.Header("X-Decision-Tenant-Mode", "all-active")
			c.JSON(200, gin.H{"profile": "local-development", "tenant_mode": "all-active", "default_tenant_id": a.tenants[0], "tenant_id": a.tenants[0], "scenario_authoring": true, "publication": true, "synchronous_execution": true, "test_run_comparison": true, "scheduled_async_execution": true, "history": true, "workflow_rule_authoring": true, "screening_configuration": true, "screening_status": true, "scoring_status": true, "investigator_review": true, "expression_assistance": gin.H{"backend": "local-expression-assistant", "uses_ai": false}, "workflow_delivery": true, "screening": true, "additional_scoring": true, "callbacks": true, "outbound_events": true, "geoip": false, "delivery_backend": "local-development", "callback_url": localdelivery.CallbackURL, "event_url": localdelivery.EventURL, "scoring_ruleset": "local:decision-score-v1"})
		})
		h, err := mysqlapi.NewExecutionHTTP(func(ctx context.Context, tenant uuid.UUID) (*mysqlapi.ExecutionEndpointScope, error) {
			if a.stopped.Load() {
				return nil, store.ErrOwnerContractUnavailable
			}
			scope, err := r.Scope(ctx, tenant, false)
			if err != nil {
				return nil, err
			}
			return scope.Endpoint, nil
		}, int64(p.HTTPMaxBytes))
		if err != nil {
			return fail(err)
		}
		v := engine.Group("/v1")
		localMiddleware := func(c *gin.Context) {
			if c.GetHeader("X-Decision-Profile") == "1" {
				c.Request = c.Request.WithContext(ports.WithEvaluationTrace(c.Request.Context()))
			}
			raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
			trusted := c.Request.Context()
			machine := subtle.ConstantTimeCompare([]byte(raw), []byte(token)) == 1
			if machine {
				trusted = context.WithValue(trusted, localCredential{}, token)
			} else {
				var err error
				trusted, err = frontendIdentity.Authenticate(c.Request)
				if err != nil {
					c.AbortWithStatusJSON(401, gin.H{"error": "authentication_required"})
					return
				}
			}
			if raw := c.Param("tenantId"); raw != "" {
				tenant, err := uuid.Parse(raw)
				if err != nil || tenant == uuid.Nil {
					c.AbortWithStatusJSON(400, gin.H{"error": "invalid_tenant"})
					return
				}
				started := time.Now()
				err = registry.CheckActive(c.Request.Context(), tenant)
				ports.RecordEvaluationDuration(c.Request.Context(), "tenant_auth", time.Since(started))
				if errors.Is(err, localtenants.ErrDenied) {
					c.AbortWithStatusJSON(403, gin.H{"error": "forbidden"})
					return
				}
				if err != nil {
					c.AbortWithStatusJSON(503, gin.H{"error": "tenant_registry_unavailable"})
					return
				}
			}
			path := c.FullPath()
			bounded, cancel := context.WithTimeout(trusted, p.RequestTimeout)
			defer cancel()
			if machine && (strings.Contains(path, "/investigator/") || strings.Contains(path, "/workflows") || strings.Contains(path, "/workflow-rules") || strings.Contains(path, "/screening-configs")) {
				bounded = context.WithValue(bounded, localReviewCredential{}, true)
			}
			c.Request = c.Request.WithContext(bounded)
			c.Next()
		}
		v.Use(localMiddleware)
		v.GET("/service-info", func(c *gin.Context) {
			c.JSON(200, gin.H{"service": "decision-engine-service", "data_model_service_url": "http://data-model-service:8080", "ingestion_service_url": "http://ingestion-service:8081"})
		})
		h.Register(v)
		internal := engine.Group("")
		internal.Use(localMiddleware)
		h.RegisterInternal(internal)
		browser := engine.Group("/internal/workflow-authoring", localMiddleware)
		h.RegisterBrowser(browser)
		h.RegisterBrowserScreening(engine.Group("/internal/screening-authoring", localMiddleware))
	}
	a.handler = engine
	a.ready.Store(true)
	return a, db, nil
}

// LocalHandler also applies origin protection to documentation and health routes.
func (a *Application) LocalHandler() (http.Handler, error) {
	token, err := LocalSecret("decision_runtime_token")
	if err != nil {
		return nil, err
	}
	return localFrontendHandler(a.Handler(), os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN"), token)
}
