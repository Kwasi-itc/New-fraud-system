package eventstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Repository is the process-local event data repository shared by ingestion
// and decision-engine binaries. Each process owns a bounded ClickHouse HTTP
// connection pool; Valkey is optional and is used only for bounded aggregate
// fact acceleration and cross-process invalidation.
type Repository struct {
	store        clickHouseClient
	features     *featureCache
	factsEnabled bool
	logger       *slog.Logger
	buildCtx     context.Context
	cancelBuild  context.CancelFunc
	buildQueue   chan aggregateFactPlan
	pending      sync.Map
	rejected     sync.Map
	buildWG      sync.WaitGroup
}

func NewRepository(cfg Config, logger *slog.Logger) (*Repository, error) {
	if strings.TrimSpace(cfg.ClickHouseURL) == "" {
		return nil, fmt.Errorf("CLICKHOUSE_URL is required")
	}
	if cfg.MaxConns < 0 || cfg.MaxIdleConns < 0 {
		return nil, fmt.Errorf("ClickHouse connection limits must be greater than or equal to zero")
	}
	if cfg.MaxConns > 0 && cfg.MaxIdleConns > cfg.MaxConns {
		return nil, fmt.Errorf("ClickHouse max idle connections must not exceed max connections")
	}
	if logger == nil {
		logger = slog.Default()
	}
	buildCtx, cancelBuild := context.WithCancel(context.Background())
	repository := &Repository{
		store: newClickHouseClient(cfg), factsEnabled: !cfg.DisableAggregateFacts, logger: logger,
		buildCtx: buildCtx, cancelBuild: cancelBuild, buildQueue: make(chan aggregateFactPlan, 64),
	}
	if repository.factsEnabled {
		repository.features = newFeatureCache(cfg)
		repository.buildWG.Add(1)
		go repository.runFactBuilder()
	} else {
		logger.Info("event aggregate hourly facts disabled; using raw and batched ClickHouse aggregates")
	}
	return repository, nil
}

func (r *Repository) Initialize(ctx context.Context) error { return r.store.initialize(ctx) }

func (r *Repository) Health(ctx context.Context) error { return r.store.health(ctx) }

func (r *Repository) Close() {
	r.cancelBuild()
	r.buildWG.Wait()
	r.store.close()
}

func (r *Repository) Write(ctx context.Context, table TableContract, event Event) error {
	if err := table.validate(); err != nil {
		return err
	}
	if err := event.validate(table); err != nil {
		return err
	}
	if err := r.store.insert(ctx, table, []Event{event}); err != nil {
		return err
	}
	return nil
}

func (r *Repository) WriteBatch(ctx context.Context, table TableContract, events []Event) error {
	if len(events) == 0 || len(events) > 500 {
		return fmt.Errorf("events must contain 1 to 500 records")
	}
	if err := table.validate(); err != nil {
		return err
	}
	for _, event := range events {
		if err := event.validate(table); err != nil {
			return err
		}
	}
	if err := r.store.insert(ctx, table, events); err != nil {
		return err
	}
	return nil
}

func (r *Repository) GetRecord(ctx context.Context, request RecordRequest) (map[string]any, error) {
	return r.store.getRecord(ctx, request)
}

func (r *Repository) ListRecords(ctx context.Context, request RecordRequest) ([]map[string]any, error) {
	return r.store.listRecords(ctx, request)
}

func (r *Repository) Aggregate(ctx context.Context, request AggregateRequest) (any, error) {
	if err := ValidateAggregateRequest(request); err != nil {
		return nil, err
	}
	if !r.factsEnabled {
		return r.store.aggregate(ctx, request)
	}
	plan, eligible := planAggregateFacts(request)
	if !eligible {
		return r.store.aggregate(ctx, request)
	}
	policy := aggregatePolicyForPlan(plan)
	if policy.Mode == AggregationModeProjectionOnly {
		return r.store.aggregate(ctx, request)
	}
	plan.TemplateWide = policy.usesTemplateWideSummary()
	promoted, err := r.store.factShapePromoted(ctx, plan)
	if err == nil && promoted {
		value, factErr := r.store.aggregateFromFacts(ctx, plan, r.features, policy.forceValkeyAdmission())
		if factErr == nil {
			r.logAggregatePolicy(request, policy, "durable_summary", nil)
			return value, nil
		}
		r.logger.Warn("sealed aggregate fact lookup failed; using raw ClickHouse fallback",
			"tenant_id", request.Table.TenantID, "object_type", request.Table.ObjectType,
			"template_hash", plan.TemplateHash, "error", factErr)
	}
	if value, handled, coldErr := r.handleColdAggregate(ctx, plan, policy); handled {
		r.logAggregatePolicy(request, policy, "cold_"+policy.ColdBehavior, coldErr)
		return value, coldErr
	}
	started := time.Now()
	value, err := r.store.aggregate(ctx, request)
	if err != nil {
		return nil, err
	}
	r.observeRawAggregate(ctx, plan, policy, time.Since(started))
	r.logAggregatePolicy(request, policy, "raw_clickhouse", nil)
	return value, nil
}

func (r *Repository) AggregateBatch(ctx context.Context, requests []AggregateRequest) ([]any, error) {
	if len(requests) == 0 || len(requests) > 64 {
		return nil, fmt.Errorf("aggregate batch must contain 1 to 64 requests")
	}
	first := requests[0].Table
	for _, request := range requests {
		if err := ValidateAggregateRequest(request); err != nil {
			return nil, err
		}
		if request.Table.TenantID != first.TenantID || request.Table.TableID != first.TableID || request.Table.SchemaRevision != first.SchemaRevision {
			return nil, fmt.Errorf("aggregate batch requests must target one event table revision")
		}
	}
	if !r.factsEnabled {
		return r.store.aggregateBatch(ctx, requests)
	}

	values := make([]any, len(requests))
	rawRequests := make([]AggregateRequest, 0, len(requests))
	rawIndexes := make([]int, 0, len(requests))
	rawPlans := make([]aggregateFactPlan, 0, len(requests))
	rawPolicies := make([]aggregateRuntimePolicy, 0, len(requests))
	for i, request := range requests {
		plan, eligible := planAggregateFacts(request)
		if !eligible {
			rawRequests = append(rawRequests, request)
			rawIndexes = append(rawIndexes, i)
			rawPlans = append(rawPlans, aggregateFactPlan{})
			rawPolicies = append(rawPolicies, aggregateRuntimePolicy{})
			continue
		}
		policy := aggregatePolicyForPlan(plan)
		if policy.Mode == AggregationModeProjectionOnly {
			rawRequests = append(rawRequests, request)
			rawIndexes = append(rawIndexes, i)
			rawPlans = append(rawPlans, aggregateFactPlan{})
			rawPolicies = append(rawPolicies, aggregateRuntimePolicy{})
			continue
		}
		plan.TemplateWide = policy.usesTemplateWideSummary()
		promoted, err := r.store.factShapePromoted(ctx, plan)
		if err == nil && promoted {
			value, factErr := r.store.aggregateFromFacts(ctx, plan, r.features, policy.forceValkeyAdmission())
			if factErr == nil {
				values[i] = value
				r.logAggregatePolicy(request, policy, "durable_summary", nil)
				continue
			}
			r.logger.Warn("batched sealed aggregate fact lookup failed; using configured cold behavior",
				"tenant_id", request.Table.TenantID, "object_type", request.Table.ObjectType,
				"template_hash", plan.TemplateHash, "error", factErr)
		}
		if value, handled, coldErr := r.handleColdAggregate(ctx, plan, policy); handled {
			if errors.Is(coldErr, ErrAggregationSkipped) {
				values[i] = nil
				r.logAggregatePolicy(request, policy, "cold_"+policy.ColdBehavior, nil)
				continue
			}
			if coldErr != nil {
				return nil, coldErr
			}
			values[i] = value
			r.logAggregatePolicy(request, policy, "cold_"+policy.ColdBehavior, nil)
			continue
		}
		rawRequests = append(rawRequests, request)
		rawIndexes = append(rawIndexes, i)
		rawPlans = append(rawPlans, plan)
		rawPolicies = append(rawPolicies, policy)
	}
	if len(rawRequests) == 0 {
		return values, nil
	}
	started := time.Now()
	rawValues, err := r.store.aggregateBatch(ctx, rawRequests)
	if err != nil {
		return nil, err
	}
	elapsed := time.Since(started)
	for i, value := range rawValues {
		values[rawIndexes[i]] = value
		if rawPolicies[i].Mode != "" {
			r.observeRawAggregate(ctx, rawPlans[i], rawPolicies[i], elapsed)
			r.logAggregatePolicy(rawRequests[i], rawPolicies[i], "raw_clickhouse_batch", nil)
		}
	}
	return values, nil
}

func (r *Repository) handleColdAggregate(ctx context.Context, plan aggregateFactPlan, policy aggregateRuntimePolicy) (any, bool, error) {
	if policy.Mode == AggregationModeTieredSummary || policy.Mode == AggregationModeAlwaysOnline {
		r.enqueueFactBuild(plan)
	}
	switch policy.ColdBehavior {
	case AggregationColdQueryClickHouse:
		return nil, false, nil
	case AggregationColdDurableSummary:
		if err := r.store.promoteFactShape(ctx, plan); err != nil {
			return nil, true, err
		}
		value, err := r.store.aggregateFromFacts(ctx, plan, r.features, policy.forceValkeyAdmission())
		return value, true, err
	case AggregationColdDeferAsync:
		r.enqueueFactBuild(plan)
		return nil, true, ErrAggregationDeferred
	case AggregationColdSkipRule:
		r.observeColdWithoutQuery(ctx, plan, policy)
		return nil, true, ErrAggregationSkipped
	case AggregationColdUseDefault:
		r.observeColdWithoutQuery(ctx, plan, policy)
		if policy.DefaultValue == nil {
			return nil, true, fmt.Errorf("aggregation field %s is missing its configured default value", policy.Field)
		}
		return *policy.DefaultValue, true, nil
	default:
		return nil, true, fmt.Errorf("unsupported aggregation cold behavior %q", policy.ColdBehavior)
	}
}

func (r *Repository) observeRawAggregate(ctx context.Context, plan aggregateFactPlan, policy aggregateRuntimePolicy, elapsed time.Duration) {
	if policy.Mode != AggregationModeAdaptiveCache {
		return
	}
	if r.features.shouldPromoteTemplate(ctx, plan.Request.Table.TenantID, aggregateFactShapeKey(plan), elapsed) {
		r.enqueueFactBuild(plan)
	}
}

func (r *Repository) observeColdWithoutQuery(ctx context.Context, plan aggregateFactPlan, policy aggregateRuntimePolicy) {
	if policy.Mode == AggregationModeAdaptiveCache &&
		r.features.shouldPromoteTemplate(ctx, plan.Request.Table.TenantID, aggregateFactShapeKey(plan), 0) {
		r.enqueueFactBuild(plan)
	}
}

func (r *Repository) logAggregatePolicy(request AggregateRequest, policy aggregateRuntimePolicy, source string, err error) {
	attrs := []any{
		"tenant_id", request.Table.TenantID, "object_type", request.Table.ObjectType,
		"field", policy.Field, "aggregation_mode", policy.Mode,
		"cold_behavior", policy.ColdBehavior, "source", source,
	}
	if err != nil {
		attrs = append(attrs, "error", err)
		r.logger.Warn("event aggregation policy applied", attrs...)
		return
	}
	r.logger.Debug("event aggregation policy applied", attrs...)
}

func (r *Repository) enqueueFactBuild(plan aggregateFactPlan) {
	shapeKey := aggregateFactShapeKey(plan)
	if _, rejected := r.rejected.Load(shapeKey); rejected {
		return
	}
	if _, loaded := r.pending.LoadOrStore(shapeKey, struct{}{}); loaded {
		return
	}
	select {
	case r.buildQueue <- plan:
	case <-r.buildCtx.Done():
		r.pending.Delete(shapeKey)
	default:
		r.pending.Delete(shapeKey)
		r.logger.Warn("sealed aggregate fact build queue is full", "template_hash", plan.TemplateHash, "series_hash", shapeKey)
	}
}

func (r *Repository) runFactBuilder() {
	defer r.buildWG.Done()
	for {
		select {
		case <-r.buildCtx.Done():
			return
		case plan := <-r.buildQueue:
			func() {
				shapeKey := aggregateFactShapeKey(plan)
				defer r.pending.Delete(shapeKey)
				ctx, cancel := context.WithTimeout(r.buildCtx, 10*time.Minute)
				defer cancel()
				if err := r.store.promoteFactShape(ctx, plan); err != nil {
					if errors.Is(err, errFactPromotionLimit) {
						r.rejected.Store(shapeKey, struct{}{})
					}
					r.logger.Warn("sealed aggregate fact promotion failed",
						"tenant_id", plan.Request.Table.TenantID, "object_type", plan.Request.Table.ObjectType,
						"template_hash", plan.TemplateHash, "series_hash", shapeKey, "error", err)
					return
				}
				r.logger.Info("sealed aggregate fact template promoted",
					"tenant_id", plan.Request.Table.TenantID, "object_type", plan.Request.Table.ObjectType,
					"template_hash", plan.TemplateHash, "series_hash", shapeKey)
			}()
		}
	}
}

func ValidateAggregateRequest(request AggregateRequest) error {
	if err := request.Table.validate(); err != nil {
		return err
	}
	if _, ok := request.Table.Fields[request.Field]; !ok {
		return fmt.Errorf("aggregate field is not in the data model")
	}
	if !hasEventTimeLowerBound(request.Filter, request.Table.EventTimeField, true) {
		return fmt.Errorf("event aggregate requires a lower-bound filter on %s", request.Table.EventTimeField)
	}
	return nil
}
