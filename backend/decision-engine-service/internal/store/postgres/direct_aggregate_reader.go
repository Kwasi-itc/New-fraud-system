package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

const maxAggregatePlanDays = 400

type DirectAggregateReader struct {
	records      ports.TenantDataReader
	db           queryable
	models       ports.DataModelReader
	cache        ports.AggregateCache
	queryTimeout time.Duration
	semaphore    chan struct{}
	now          func() time.Time
}

func NewDirectAggregateReader(
	records ports.TenantDataReader,
	db queryable,
	models ports.DataModelReader,
	cache ports.AggregateCache,
	queryTimeout time.Duration,
	maxConcurrency int,
) DirectAggregateReader {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 16
	}
	return DirectAggregateReader{
		records:      records,
		db:           db,
		models:       models,
		cache:        cache,
		queryTimeout: queryTimeout,
		semaphore:    make(chan struct{}, maxConcurrency),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (r DirectAggregateReader) AllowLocalAggregateFallback() bool { return false }

func (r DirectAggregateReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	return r.records.GetRecord(ctx, tenantID, objectType, objectID)
}

func (r DirectAggregateReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	return r.records.ListRecords(ctx, tenantID, objectType, limit)
}

func (r DirectAggregateReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	return r.records.QueryRecords(ctx, tenantID, objectType, fieldName, value, limit)
}

func (r DirectAggregateReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	if r.db == nil || r.models == nil {
		return nil, fmt.Errorf("direct aggregate reader is not configured")
	}
	select {
	case r.semaphore <- struct{}{}:
		defer func() { <-r.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	queryCtx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	model, err := r.models.GetTenantModel(queryCtx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load tenant model for aggregate: %w", err)
	}
	table, err := resolveAggregateTable(model, query.ObjectType)
	if err != nil {
		return nil, err
	}
	field, err := resolveAggregateField(model, table, query.Field)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(query.Aggregate, "count_distinct") {
		return nil, fmt.Errorf("count_distinct is not supported by direct aggregate execution")
	}

	now := r.now()
	plan, ok := buildBucketPlan(model, table, query, now)
	if !ok || r.cache == nil {
		component, err := r.queryComponent(queryCtx, model, table, field, query)
		if err != nil {
			return nil, err
		}
		return component.result(), nil
	}

	combined := aggregateComponent{Aggregate: normalizedAggregate(query.Aggregate)}
	for _, part := range plan.Parts {
		partQuery := query
		baseFilter := query.Filter
		if part.Cacheable {
			baseFilter = plan.NonTimeFilter
		}
		partQuery.Filter = intersectWithRange(baseFilter, plan.Definition.TimestampFieldName, part.Start, part.End)

		var component aggregateComponent
		if part.Cacheable {
			component, err = r.cachedBucketComponent(queryCtx, tenantID, model, table, field, plan.Definition, part.Start, partQuery)
		} else {
			component, err = r.queryComponent(queryCtx, model, table, field, partQuery)
		}
		if err != nil {
			return nil, err
		}
		if err := combined.merge(component, field.Type); err != nil {
			return nil, err
		}
	}
	return combined.result(), nil
}

func (r DirectAggregateReader) cachedBucketComponent(
	ctx context.Context,
	tenantID string,
	model ports.TenantModel,
	table ports.TenantModelTable,
	field ports.TenantModelField,
	definition ports.LogicalBucketDefinition,
	bucketStart time.Time,
	query ports.AggregateQuery,
) (aggregateComponent, error) {
	for attempt := 0; attempt < 2; attempt++ {
		generation, err := r.readGeneration(ctx, tenantID, table.ID, definition, bucketStart)
		if err != nil {
			slog.Default().Warn("aggregate generation read failed; querying bucket directly", "error", err)
			return r.queryComponent(ctx, model, table, field, query)
		}
		key, err := aggregateCacheKey(tenantID, model, table, definition, bucketStart, generation, query)
		if err != nil {
			return aggregateComponent{}, err
		}
		cacheCtx, cancelCache := context.WithTimeout(ctx, 100*time.Millisecond)
		payload, found, cacheErr := r.cache.Get(cacheCtx, key)
		cancelCache()
		if cacheErr != nil {
			slog.Default().Warn("aggregate cache read failed; querying bucket directly", "error", cacheErr)
		}
		if cacheErr == nil && found {
			var component aggregateComponent
			if json.Unmarshal(payload, &component) == nil {
				confirmedGeneration, confirmErr := r.readGeneration(ctx, tenantID, table.ID, definition, bucketStart)
				if confirmErr == nil && confirmedGeneration == generation {
					return component, nil
				}
				if attempt == 0 {
					continue
				}
			}
		}

		component, err := r.queryComponent(ctx, model, table, field, query)
		if err != nil {
			return aggregateComponent{}, err
		}
		confirmedGeneration, err := r.readGeneration(ctx, tenantID, table.ID, definition, bucketStart)
		if err != nil || confirmedGeneration != generation {
			if attempt == 0 {
				continue
			}
			return component, nil
		}
		payload, err = json.Marshal(component)
		if err != nil {
			return aggregateComponent{}, err
		}
		cacheCtx, cancelCache = context.WithTimeout(ctx, 100*time.Millisecond)
		cacheErr = r.cache.Set(cacheCtx, key, payload)
		cancelCache()
		if cacheErr != nil {
			slog.Default().Warn("aggregate cache write failed; returning database result", "error", cacheErr)
		}
		return component, nil
	}
	return r.queryComponent(ctx, model, table, field, query)
}

func (r DirectAggregateReader) readGeneration(
	ctx context.Context,
	tenantID string,
	tableID string,
	definition ports.LogicalBucketDefinition,
	bucketStart time.Time,
) (int64, error) {
	const stmt = `
		SELECT generation
		FROM core_ingestion.logical_bucket_generations
		WHERE tenant_id = $1
		  AND table_id = $2
		  AND bucket_definition_id = $3
		  AND definition_version = $4
		  AND bucket_start_utc = $5
	`
	var generation int64
	err := r.db.QueryRow(ctx, stmt, tenantID, tableID, definition.ID, definition.DefinitionVersion, bucketStart).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return generation, err
}

type aggregateComponent struct {
	Aggregate string  `json:"aggregate"`
	Count     int64   `json:"count,omitempty"`
	Number    float64 `json:"number,omitempty"`
	Scalar    any     `json:"scalar,omitempty"`
	HasScalar bool    `json:"has_scalar,omitempty"`
}

func (c aggregateComponent) result() any {
	switch c.Aggregate {
	case "count":
		return c.Count
	case "sum":
		return c.Number
	case "avg":
		if c.Count == 0 {
			return nil
		}
		return c.Number / float64(c.Count)
	case "min", "max":
		if !c.HasScalar {
			return nil
		}
		return c.Scalar
	default:
		return nil
	}
}

func (c *aggregateComponent) merge(other aggregateComponent, fieldType string) error {
	if c.Aggregate == "" {
		c.Aggregate = other.Aggregate
	}
	if c.Aggregate != other.Aggregate {
		return fmt.Errorf("cannot merge different aggregate components")
	}
	switch c.Aggregate {
	case "count":
		c.Count += other.Count
	case "sum":
		c.Number += other.Number
	case "avg":
		c.Number += other.Number
		c.Count += other.Count
	case "min", "max":
		if !other.HasScalar {
			return nil
		}
		if !c.HasScalar {
			c.Scalar, c.HasScalar = other.Scalar, true
			return nil
		}
		comparison, err := compareAggregateScalars(c.Scalar, other.Scalar, fieldType)
		if err != nil {
			return err
		}
		if (c.Aggregate == "min" && comparison > 0) || (c.Aggregate == "max" && comparison < 0) {
			c.Scalar = other.Scalar
		}
	default:
		return fmt.Errorf("aggregate %q is not supported", c.Aggregate)
	}
	return nil
}

func (r DirectAggregateReader) queryComponent(
	ctx context.Context,
	model ports.TenantModel,
	table ports.TenantModelTable,
	field ports.TenantModelField,
	query ports.AggregateQuery,
) (aggregateComponent, error) {
	aggregate := normalizedAggregate(query.Aggregate)
	expression, scanCount, err := directAggregateExpression(aggregate, field)
	if err != nil {
		return aggregateComponent{}, err
	}
	args := []any{}
	whereSQL := ""
	if query.Filter != nil {
		filterSQL, err := buildDirectAggregateFilter(model, table, *query.Filter, &args)
		if err != nil {
			return aggregateComponent{}, err
		}
		if filterSQL != "" {
			whereSQL = " WHERE " + filterSQL
		}
	}
	if strings.TrimSpace(model.PhysicalSchemaName) == "" {
		return aggregateComponent{}, fmt.Errorf("published tenant model is missing physical_schema_name")
	}
	sql := fmt.Sprintf(
		"SELECT %s FROM %s%s",
		expression,
		pgx.Identifier{model.PhysicalSchemaName, table.Name}.Sanitize(),
		whereSQL,
	)
	component := aggregateComponent{Aggregate: aggregate}
	if scanCount == 2 {
		if err := r.db.QueryRow(ctx, sql, args...).Scan(&component.Number, &component.Count); err != nil {
			return aggregateComponent{}, fmt.Errorf("execute direct aggregate: %w", err)
		}
		return component, nil
	}

	var value any
	if err := r.db.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
		return aggregateComponent{}, fmt.Errorf("execute direct aggregate: %w", err)
	}
	switch aggregate {
	case "count":
		component.Count, err = int64Value(value)
	case "sum":
		component.Number, err = float64Value(value)
	case "min", "max":
		component.Scalar, component.HasScalar = normalizeAggregateScalar(value, field.Type)
	}
	if err != nil {
		return aggregateComponent{}, err
	}
	return component, nil
}

func directAggregateExpression(aggregate string, field ports.TenantModelField) (string, int, error) {
	column := pgx.Identifier{field.Name}.Sanitize()
	switch aggregate {
	case "count":
		return fmt.Sprintf("COUNT(%s)::bigint", column), 1, nil
	case "sum":
		if field.Type != "int" && field.Type != "float" {
			return "", 0, fmt.Errorf("sum requires an int or float field")
		}
		return fmt.Sprintf("COALESCE(SUM(%s), 0)::double precision", column), 1, nil
	case "avg":
		if field.Type != "int" && field.Type != "float" {
			return "", 0, fmt.Errorf("avg requires an int or float field")
		}
		return fmt.Sprintf("COALESCE(SUM(%s), 0)::double precision, COUNT(%s)::bigint", column, column), 2, nil
	case "min", "max":
		function := strings.ToUpper(aggregate)
		switch field.Type {
		case "int", "float":
			return fmt.Sprintf("%s(%s)::double precision", function, column), 1, nil
		case "timestamp":
			return fmt.Sprintf("%s(%s)", function, column), 1, nil
		default:
			return fmt.Sprintf("%s(%s)::text", function, column), 1, nil
		}
	default:
		return "", 0, fmt.Errorf("aggregate %q is not supported", aggregate)
	}
}

func buildDirectAggregateFilter(
	model ports.TenantModel,
	table ports.TenantModelTable,
	filter ports.AggregateFilter,
	args *[]any,
) (string, error) {
	switch strings.ToLower(strings.TrimSpace(filter.Kind)) {
	case "", "group":
		operator := strings.ToLower(strings.TrimSpace(filter.Operator))
		if operator == "" {
			operator = "and"
		}
		if operator == "not" {
			if len(filter.Children) != 1 {
				return "", fmt.Errorf("not filter expects exactly one child")
			}
			child, err := buildDirectAggregateFilter(model, table, filter.Children[0], args)
			if err != nil {
				return "", err
			}
			return "NOT (" + child + ")", nil
		}
		if operator != "and" && operator != "or" {
			return "", fmt.Errorf("filter group operator %q is not supported", filter.Operator)
		}
		parts := make([]string, 0, len(filter.Children))
		for _, child := range filter.Children {
			part, err := buildDirectAggregateFilter(model, table, child, args)
			if err != nil {
				return "", err
			}
			if part != "" {
				parts = append(parts, "("+part+")")
			}
		}
		return strings.Join(parts, " "+strings.ToUpper(operator)+" "), nil
	case "predicate":
		field, err := resolveAggregateField(model, table, filter.Field)
		if err != nil {
			return "", err
		}
		column := pgx.Identifier{field.Name}.Sanitize()
		switch strings.ToLower(strings.TrimSpace(filter.Op)) {
		case "eq", "neq", "gt", "gte", "lt", "lte":
			operators := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
			*args = append(*args, filter.Value)
			return fmt.Sprintf("%s %s $%d", column, operators[strings.ToLower(filter.Op)], len(*args)), nil
		case "in":
			items, ok := filter.Value.([]any)
			if !ok || len(items) == 0 {
				return "", fmt.Errorf("in filter expects a non-empty list value")
			}
			placeholders := make([]string, 0, len(items))
			for _, item := range items {
				*args = append(*args, item)
				placeholders = append(placeholders, "$"+strconv.Itoa(len(*args)))
			}
			return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), nil
		case "is_null":
			return column + " IS NULL", nil
		case "is_not_null":
			return column + " IS NOT NULL", nil
		case "starts_with", "ends_with":
			value, ok := filter.Value.(string)
			if !ok {
				return "", fmt.Errorf("%s expects a string value", filter.Op)
			}
			if strings.EqualFold(filter.Op, "starts_with") {
				value += "%"
			} else {
				value = "%" + value
			}
			*args = append(*args, value)
			return fmt.Sprintf("%s LIKE $%d", column, len(*args)), nil
		default:
			return "", fmt.Errorf("filter operator %q is not supported", filter.Op)
		}
	default:
		return "", fmt.Errorf("filter kind %q is not supported", filter.Kind)
	}
}

type aggregateBucketPlan struct {
	Definition    ports.LogicalBucketDefinition
	NonTimeFilter *ports.AggregateFilter
	Parts         []aggregatePlanPart
}

type aggregatePlanPart struct {
	Start     time.Time
	End       time.Time
	Cacheable bool
}

type aggregateTimeBounds struct {
	Lower          time.Time
	Upper          time.Time
	LowerInclusive bool
	UpperInclusive bool
}

func buildBucketPlan(
	model ports.TenantModel,
	table ports.TenantModelTable,
	query ports.AggregateQuery,
	now time.Time,
) (aggregateBucketPlan, bool) {
	if query.Filter == nil {
		return aggregateBucketPlan{}, false
	}
	for _, definition := range model.LogicalBuckets {
		if definition.TableID != table.ID ||
			definition.Status != "active" ||
			definition.Grain != "daily" ||
			definition.CacheEligibleAt == nil ||
			now.Before(*definition.CacheEligibleAt) {
			continue
		}
		nonTimeFilter, bounds, ok := extractTimeBounds(*query.Filter, definition.TimestampFieldName)
		if !ok || !bounds.Lower.Before(bounds.Upper) {
			continue
		}
		location, err := time.LoadLocation(definition.Timezone)
		if err != nil {
			continue
		}
		start := logicalDayStart(bounds.Lower, location)
		parts := make([]aggregatePlanPart, 0)
		for len(parts) <= maxAggregatePlanDays && start.Before(bounds.Upper) {
			end := nextLogicalDay(start, location)
			full := (bounds.Lower.Before(start) || (bounds.Lower.Equal(start) && bounds.LowerInclusive)) &&
				(bounds.Upper.After(end) || bounds.Upper.Equal(end))
			sealed := !now.Before(end.Add(definition.SealDelay))
			cacheable := full && sealed
			if len(parts) > 0 && !cacheable && !parts[len(parts)-1].Cacheable {
				parts[len(parts)-1].End = end
			} else {
				parts = append(parts, aggregatePlanPart{Start: start, End: end, Cacheable: cacheable})
			}
			start = end
		}
		if len(parts) == 0 || start.Before(bounds.Upper) {
			continue
		}
		return aggregateBucketPlan{
			Definition:    definition,
			NonTimeFilter: nonTimeFilter,
			Parts:         parts,
		}, true
	}
	return aggregateBucketPlan{}, false
}

func extractTimeBounds(filter ports.AggregateFilter, fieldName string) (*ports.AggregateFilter, aggregateTimeBounds, bool) {
	predicates, ok := flattenAndFilter(filter)
	if !ok {
		return nil, aggregateTimeBounds{}, false
	}
	var bounds aggregateTimeBounds
	hasLower, hasUpper := false, false
	remaining := make([]ports.AggregateFilter, 0, len(predicates))
	for _, predicate := range predicates {
		if predicate.Kind != "predicate" || predicate.Field != fieldName {
			remaining = append(remaining, predicate)
			continue
		}
		value, ok := timeValue(predicate.Value)
		if !ok {
			return nil, aggregateTimeBounds{}, false
		}
		switch strings.ToLower(predicate.Op) {
		case "gt", "gte":
			inclusive := strings.EqualFold(predicate.Op, "gte")
			if !hasLower || value.After(bounds.Lower) || (value.Equal(bounds.Lower) && !inclusive) {
				bounds.Lower, bounds.LowerInclusive = value, inclusive
			}
			hasLower = true
		case "lt", "lte":
			inclusive := strings.EqualFold(predicate.Op, "lte")
			if !hasUpper || value.Before(bounds.Upper) || (value.Equal(bounds.Upper) && !inclusive) {
				bounds.Upper, bounds.UpperInclusive = value, inclusive
			}
			hasUpper = true
		default:
			return nil, aggregateTimeBounds{}, false
		}
	}
	if !hasLower || !hasUpper {
		return nil, aggregateTimeBounds{}, false
	}
	if len(remaining) == 0 {
		return nil, bounds, true
	}
	if len(remaining) == 1 {
		return &remaining[0], bounds, true
	}
	return &ports.AggregateFilter{Kind: "group", Operator: "and", Children: remaining}, bounds, true
}

func flattenAndFilter(filter ports.AggregateFilter) ([]ports.AggregateFilter, bool) {
	if filter.Kind == "predicate" {
		return []ports.AggregateFilter{filter}, true
	}
	if filter.Kind != "" && filter.Kind != "group" {
		return nil, false
	}
	operator := strings.ToLower(strings.TrimSpace(filter.Operator))
	if operator == "" {
		operator = "and"
	}
	if operator != "and" {
		return []ports.AggregateFilter{filter}, true
	}
	var out []ports.AggregateFilter
	for _, child := range filter.Children {
		items, ok := flattenAndFilter(child)
		if !ok {
			return nil, false
		}
		out = append(out, items...)
	}
	return out, true
}

func intersectWithRange(
	filter *ports.AggregateFilter,
	fieldName string,
	start, end time.Time,
) *ports.AggregateFilter {
	children := make([]ports.AggregateFilter, 0, 3)
	if filter != nil {
		children = append(children, *filter)
	}
	children = append(children,
		ports.AggregateFilter{Kind: "predicate", Field: fieldName, Op: "gte", Value: start},
		ports.AggregateFilter{Kind: "predicate", Field: fieldName, Op: "lt", Value: end},
	)
	return &ports.AggregateFilter{Kind: "group", Operator: "and", Children: children}
}

func aggregateCacheKey(
	tenantID string,
	model ports.TenantModel,
	table ports.TenantModelTable,
	definition ports.LogicalBucketDefinition,
	bucketStart time.Time,
	generation int64,
	query ports.AggregateQuery,
) (string, error) {
	signature, err := json.Marshal(struct {
		Aggregate string                 `json:"aggregate"`
		Field     string                 `json:"field"`
		Filter    *ports.AggregateFilter `json:"filter,omitempty"`
	}{
		Aggregate: normalizedAggregate(query.Aggregate),
		Field:     query.Field,
		Filter:    query.Filter,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(signature)
	return fmt.Sprintf(
		"aggregate:v1:%s:%s:%s:%s:%d:%s:%d:%s",
		tenantID,
		model.RevisionID,
		table.ID,
		definition.ID,
		definition.DefinitionVersion,
		bucketStart.UTC().Format(time.RFC3339),
		generation,
		hex.EncodeToString(sum[:]),
	), nil
}

func resolveAggregateTable(model ports.TenantModel, objectType string) (ports.TenantModelTable, error) {
	if table, ok := model.Tables[objectType]; ok {
		return table, nil
	}
	for _, table := range model.Tables {
		if table.Name == objectType {
			return table, nil
		}
	}
	return ports.TenantModelTable{}, fmt.Errorf("object type %s is not available", objectType)
}

func resolveAggregateField(
	model ports.TenantModel,
	table ports.TenantModelTable,
	fieldName string,
) (ports.TenantModelField, error) {
	if field, ok := table.Fields[fieldName]; ok {
		return field, nil
	}
	for _, field := range table.Fields {
		if field.Name == fieldName {
			return field, nil
		}
	}
	if fieldName == model.RecordLookupField {
		return ports.TenantModelField{Name: fieldName, Type: "string"}, nil
	}
	return ports.TenantModelField{}, fmt.Errorf("field %s is not available on object type %s", fieldName, table.Name)
}

func normalizedAggregate(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func logicalDayStart(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
}

func nextLogicalDay(start time.Time, location *time.Location) time.Time {
	local := start.In(location)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location).UTC()
}

func timeValue(value any) (time.Time, bool) {
	switch item := value.(type) {
	case time.Time:
		return item.UTC(), true
	case *time.Time:
		if item != nil {
			return item.UTC(), true
		}
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, item)
		return parsed.UTC(), err == nil
	}
	return time.Time{}, false
}

func int64Value(value any) (int64, error) {
	switch item := value.(type) {
	case int64:
		return item, nil
	case int32:
		return int64(item), nil
	case int:
		return int64(item), nil
	case float64:
		return int64(item), nil
	case string:
		return strconv.ParseInt(item, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert aggregate value %T to int64", value)
	}
}

func float64Value(value any) (float64, error) {
	switch item := value.(type) {
	case float64:
		return item, nil
	case float32:
		return float64(item), nil
	case int64:
		return float64(item), nil
	case int32:
		return float64(item), nil
	case int:
		return float64(item), nil
	case string:
		return strconv.ParseFloat(item, 64)
	default:
		return 0, fmt.Errorf("cannot convert aggregate value %T to float64", value)
	}
}

func normalizeAggregateScalar(value any, fieldType string) (any, bool) {
	if value == nil {
		return nil, false
	}
	switch fieldType {
	case "int", "float":
		number, err := float64Value(value)
		return number, err == nil
	case "timestamp":
		timestamp, ok := timeValue(value)
		if !ok {
			return nil, false
		}
		return timestamp.Format(time.RFC3339Nano), true
	default:
		return fmt.Sprint(value), true
	}
}

func compareAggregateScalars(left, right any, fieldType string) (int, error) {
	switch fieldType {
	case "int", "float":
		leftNumber, err := float64Value(left)
		if err != nil {
			return 0, err
		}
		rightNumber, err := float64Value(right)
		if err != nil {
			return 0, err
		}
		switch {
		case math.Abs(leftNumber-rightNumber) < 1e-12:
			return 0, nil
		case leftNumber < rightNumber:
			return -1, nil
		default:
			return 1, nil
		}
	case "timestamp":
		leftTime, leftOK := timeValue(left)
		rightTime, rightOK := timeValue(right)
		if !leftOK || !rightOK {
			return 0, fmt.Errorf("cannot compare timestamp aggregate values")
		}
		switch {
		case leftTime.Equal(rightTime):
			return 0, nil
		case leftTime.Before(rightTime):
			return -1, nil
		default:
			return 1, nil
		}
	default:
		return strings.Compare(fmt.Sprint(left), fmt.Sprint(right)), nil
	}
}
