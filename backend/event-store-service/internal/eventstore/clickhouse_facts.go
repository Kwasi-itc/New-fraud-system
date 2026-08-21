package eventstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errFactPromotionLimit = errors.New("sealed aggregate fact promotion limit reached")

const (
	factSourceTable     = "event_fact_sources"
	factGenerationTable = "event_fact_bucket_generations"
	factShapeTable      = "event_fact_shapes"
	factBuildTable      = "event_fact_bucket_builds"
	factValueTable      = "event_fact_bucket_values"
)

func (c clickHouseClient) initializeAggregateFactTables(ctx context.Context) error {
	queries := []string{
		"CREATE TABLE IF NOT EXISTS " + c.database + "." + factSourceTable + ` (
tenant_id String,
table_id String,
schema_revision String,
initialized_at DateTime64(9, 'UTC'),
version UInt64
) ENGINE = ReplacingMergeTree(version)
ORDER BY (tenant_id, table_id, schema_revision)`,
		// Bucket generations are additive invalidation counters. The
		// materialized view contributes one count for every inserted block
		// touching an hour, so timestamp ordering cannot hide a late write.
		"CREATE TABLE IF NOT EXISTS " + c.database + "." + factGenerationTable + ` (
tenant_id String,
table_id String,
schema_revision String,
bucket_start DateTime('UTC'),
generation UInt64
) ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(bucket_start)
ORDER BY (tenant_id, table_id, schema_revision, bucket_start)`,
		"CREATE TABLE IF NOT EXISTS " + c.database + "." + factShapeTable + ` (
tenant_id String,
table_id String,
schema_revision String,
template_hash String,
built_at DateTime64(9, 'UTC'),
version UInt64
) ENGINE = ReplacingMergeTree(version)
ORDER BY (tenant_id, table_id, schema_revision, template_hash)`,
		"CREATE TABLE IF NOT EXISTS " + c.database + "." + factBuildTable + ` (
tenant_id String,
table_id String,
schema_revision String,
template_hash String,
bucket_start DateTime('UTC'),
generation UInt64,
built_at DateTime64(9, 'UTC')
) ENGINE = ReplacingMergeTree(generation)
PARTITION BY toYYYYMM(bucket_start)
ORDER BY (tenant_id, table_id, schema_revision, template_hash, bucket_start)`,
		"CREATE TABLE IF NOT EXISTS " + c.database + "." + factValueTable + ` (
tenant_id String,
table_id String,
schema_revision String,
template_hash String,
bucket_start DateTime('UTC'),
dimension_hash String,
sum_value Float64,
count_value UInt64,
generation UInt64,
built_at DateTime64(9, 'UTC')
) ENGINE = ReplacingMergeTree(generation)
PARTITION BY toYYYYMM(bucket_start)
ORDER BY (tenant_id, table_id, schema_revision, template_hash, bucket_start, dimension_hash)`,
	}
	for _, query := range queries {
		if _, err := c.execute(ctx, query, nil); err != nil {
			return fmt.Errorf("initialize aggregate facts: %w", err)
		}
	}
	return nil
}

func (c clickHouseClient) ensureFactGenerationView(ctx context.Context, table TableContract) error {
	viewName := "fact_generation_" + physicalTableName(table.TenantID, table.TableID)
	query := "CREATE MATERIALIZED VIEW IF NOT EXISTS " + c.database + "." + quoteIdentifier(viewName) +
		" TO " + c.database + "." + factGenerationTable + " AS SELECT " +
		quote(table.TenantID) + " AS tenant_id, " + quote(table.TableID) + " AS table_id, " +
		quote(table.SchemaRevision) + " AS schema_revision, toStartOfHour(" + quoteIdentifier(table.EventTimeField) +
		") AS bucket_start, toUInt64(1) AS generation FROM " +
		c.tableIdentifier(table) + " GROUP BY bucket_start"
	if _, err := c.execute(ctx, query, nil); err != nil {
		return fmt.Errorf("initialize aggregate fact generation view: %w", err)
	}
	return nil
}

func (c clickHouseClient) dropFactGenerationView(ctx context.Context, table TableContract) error {
	viewName := "fact_generation_" + physicalTableName(table.TenantID, table.TableID)
	_, err := c.execute(ctx, "DROP TABLE IF EXISTS "+quoteIdentifier(c.database)+"."+quoteIdentifier(viewName), nil)
	if err != nil {
		return fmt.Errorf("disable aggregate fact generation view: %w", err)
	}
	return nil
}

func (c clickHouseClient) ensureBucketGenerations(ctx context.Context, table TableContract) error {
	key := table.TenantID + ":" + table.TableID + ":" + table.SchemaRevision
	if _, ok := c.factSources.Load(key); ok {
		return nil
	}
	c.factInitMu.Lock()
	defer c.factInitMu.Unlock()
	if _, ok := c.factSources.Load(key); ok {
		return nil
	}
	where := factIdentityWhere(table)
	body, err := c.execute(ctx, "SELECT count() AS value FROM "+c.database+"."+factSourceTable+" WHERE "+where+" FORMAT JSONEachRow", nil)
	if err != nil {
		return err
	}
	var count struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &count); err != nil {
		return fmt.Errorf("decode fact source state: %w", err)
	}
	if count.Value == 0 {
		query := "INSERT INTO " + c.database + "." + factGenerationTable + " " +
			"SELECT " + quote(table.TenantID) + ", " + quote(table.TableID) + ", " + quote(table.SchemaRevision) + ", " +
			"toStartOfHour(" + quoteIdentifier(table.EventTimeField) + ") AS bucket_start, " +
			"toUInt64(1) AS generation " +
			"FROM " + c.tableIdentifier(table) + " GROUP BY bucket_start"
		if _, err := c.execute(ctx, query, nil); err != nil {
			return fmt.Errorf("bootstrap event fact generations: %w", err)
		}
		version := uint64(time.Now().UTC().UnixNano())
		var marker bytes.Buffer
		_ = json.NewEncoder(&marker).Encode(map[string]any{
			"tenant_id": table.TenantID, "table_id": table.TableID, "schema_revision": table.SchemaRevision,
			"initialized_at": time.Now().UTC().Format(clickHouseTimestampLayout), "version": version,
		})
		if _, err := c.execute(ctx, "INSERT INTO "+c.database+"."+factSourceTable+" FORMAT JSONEachRow", &marker); err != nil {
			return fmt.Errorf("record event fact source state: %w", err)
		}
	}
	c.factSources.Store(key, true)
	return nil
}

func (c clickHouseClient) factShapePromoted(ctx context.Context, plan aggregateFactPlan) (bool, error) {
	shapeKey := aggregateFactShapeKey(plan)
	if value, ok := c.factShapes.Load(shapeKey); ok {
		return value.(bool), nil
	}
	where := factIdentityWhere(plan.Request.Table) + " AND template_hash = " + quote(shapeKey)
	body, err := c.execute(ctx, "SELECT count() AS value FROM "+c.database+"."+factShapeTable+" WHERE "+where+" FORMAT JSONEachRow", nil)
	if err != nil {
		return false, err
	}
	var count struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &count); err != nil {
		return false, err
	}
	promoted := count.Value > 0
	c.factShapes.Store(shapeKey, promoted)
	return promoted, nil
}

func (c clickHouseClient) promoteFactShape(ctx context.Context, plan aggregateFactPlan) error {
	shapeKey := aggregateFactShapeKey(plan)
	identity := "tuple(tenant_id, table_id, schema_revision, template_hash)"
	query := "SELECT uniqExact(" + identity + ") AS global_count, uniqExactIf(" + identity + ", tenant_id = " +
		quote(plan.Request.Table.TenantID) + ") AS tenant_count FROM " + c.database + "." + factShapeTable + " FORMAT JSONEachRow"
	responseBody, err := c.execute(ctx, query, nil)
	if err != nil {
		return err
	}
	var counts struct {
		GlobalCount uint64 `json:"global_count"`
		TenantCount uint64 `json:"tenant_count"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(responseBody), &counts); err != nil {
		return err
	}
	if counts.GlobalCount >= uint64(c.factMaxKeys) || counts.TenantCount >= uint64(c.factMaxKeysPerTenant) {
		return errFactPromotionLimit
	}
	if err := c.ensureBucketGenerations(ctx, plan.Request.Table); err != nil {
		return err
	}
	sealedBefore := time.Now().UTC().Truncate(aggregateFactBucketSize)
	fullBucketEnd := plan.fullBucketEnd(sealedBefore)
	if plan.FullBucketStart.Before(fullBucketEnd) {
		if err := c.rebuildFactBuckets(ctx, plan, nil, fullBucketEnd); err != nil {
			return err
		}
	}
	version := uint64(time.Now().UTC().UnixNano())
	var marker bytes.Buffer
	_ = json.NewEncoder(&marker).Encode(map[string]any{
		"tenant_id": plan.Request.Table.TenantID, "table_id": plan.Request.Table.TableID,
		"schema_revision": plan.Request.Table.SchemaRevision, "template_hash": shapeKey,
		"built_at": time.Now().UTC().Format(clickHouseTimestampLayout), "version": version,
	})
	if _, err := c.execute(ctx, "INSERT INTO "+c.database+"."+factShapeTable+" FORMAT JSONEachRow", &marker); err != nil {
		return err
	}
	c.factShapes.Store(shapeKey, true)
	return nil
}

func (c clickHouseClient) aggregateFromFacts(ctx context.Context, plan aggregateFactPlan, features *featureCache, forceValkeyAdmission bool) (any, error) {
	if err := c.ensureBucketGenerations(ctx, plan.Request.Table); err != nil {
		return nil, err
	}
	sealedBefore := time.Now().UTC().Truncate(aggregateFactBucketSize)
	fullBucketEnd := plan.fullBucketEnd(sealedBefore)
	if !plan.FullBucketStart.Before(fullBucketEnd) {
		components, err := c.readRawFactComponents(ctx, plan, plan.Request.Filter)
		if err != nil {
			return nil, err
		}
		return composeAggregateFactResult(plan.Request.Aggregate, components), nil
	}
	generations, err := c.currentFactGenerations(ctx, plan, plan.FullBucketStart, fullBucketEnd)
	if err != nil {
		return nil, err
	}
	builds, err := c.currentFactBuilds(ctx, plan, plan.FullBucketStart, fullBucketEnd)
	if err != nil {
		return nil, err
	}
	stale := make([]time.Time, 0)
	for bucket, generation := range generations {
		if builds[bucket] != generation {
			stale = append(stale, bucket)
		}
	}
	if len(stale) > 0 {
		sort.Slice(stale, func(i, j int) bool { return stale[i].Before(stale[j]) })
		if err := c.rebuildFactBuckets(ctx, plan, stale, fullBucketEnd); err != nil {
			return nil, err
		}
	}

	seriesKey := aggregateSeriesKey(plan)
	series := aggregateBucketSeries{Buckets: map[string]aggregateBucketFact{}}
	seriesHit := false
	if features != nil {
		series, seriesHit = features.getSeries(ctx, plan.Request.Table.TenantID, seriesKey)
		if series.Buckets == nil {
			series.Buckets = map[string]aggregateBucketFact{}
		}
	}
	components := aggregateFactComponents{}
	missing := make([]time.Time, 0)
	for bucket, generation := range generations {
		key := bucket.Format(time.RFC3339)
		fact, ok := series.Buckets[key]
		if ok && fact.Generation == generation {
			components.Sum += fact.Sum
			components.Count += fact.Count
			continue
		}
		missing = append(missing, bucket)
	}
	if len(missing) > 0 {
		facts, err := c.readFactBuckets(ctx, plan, generations, missing)
		if err != nil {
			return nil, err
		}
		for _, bucket := range missing {
			fact := facts[bucket]
			fact.Generation = generations[bucket]
			series.Buckets[bucket.Format(time.RFC3339)] = fact
			components.Sum += fact.Sum
			components.Count += fact.Count
		}
	}
	if features != nil {
		features.observeSeries(ctx, plan.Request.Table.TenantID, seriesKey, series, seriesHit, forceValkeyAdmission)
	}
	partial, err := c.readRawFactComponents(ctx, plan, plan.partialFilter())
	if err != nil {
		return nil, err
	}
	components.Sum += partial.Sum
	components.Count += partial.Count
	upperBoundary, err := c.readRawFactComponents(ctx, plan, plan.upperBoundaryFilter(fullBucketEnd))
	if err != nil {
		return nil, err
	}
	components.Sum += upperBoundary.Sum
	components.Count += upperBoundary.Count
	return composeAggregateFactResult(plan.Request.Aggregate, components), nil
}

func (c clickHouseClient) rebuildFactBuckets(ctx context.Context, plan aggregateFactPlan, buckets []time.Time, sealedBefore time.Time) error {
	shapeKey := aggregateFactShapeKey(plan)
	generationQuery := c.factGenerationSubquery(plan.Request.Table, buckets, plan.FullBucketStart, sealedBefore)
	dimensionExprs := make([]string, len(plan.Dimensions))
	for i, dimension := range plan.Dimensions {
		dimensionExprs[i] = "toString(" + quoteIdentifier(dimension.Field) + ")"
	}
	dimensionHash := "hex(SHA256(toJSONString([" + strings.Join(dimensionExprs, ",") + "])))"
	measure := quoteIdentifier(plan.Request.Field)
	sumExpr := "toFloat64(0)"
	if plan.NumericField {
		sumExpr = "coalesce(sum(toFloat64(" + measure + ")), 0)"
	}
	where := "1 = 1"
	filterPredicates := plan.nonTimePredicates()
	if plan.TemplateWide {
		filterPredicates = nil
		if plan.StaticFilter != nil {
			if flattened, ok := flattenAndPredicates(plan.StaticFilter); ok {
				filterPredicates = flattened
			} else {
				filterPredicates = []AggregateFilter{*plan.StaticFilter}
			}
		}
	}
	if filter := andFilter(filterPredicates); filter != nil {
		filterSQL, err := buildFilterSQL(*filter, plan.Request.Table.Fields)
		if err != nil {
			return err
		}
		if filterSQL != "" {
			where += " AND (" + filterSQL + ")"
		}
	}
	bucketExpr := "toStartOfHour(" + quoteIdentifier(plan.Request.Table.EventTimeField) + ")"
	query := "INSERT INTO " + c.database + "." + factValueTable + " " +
		"SELECT " + quote(plan.Request.Table.TenantID) + ", " + quote(plan.Request.Table.TableID) + ", " +
		quote(plan.Request.Table.SchemaRevision) + ", " + quote(shapeKey) + ", " + bucketExpr + " AS bucket_start, " +
		dimensionHash + " AS dimension_hash, " + sumExpr + " AS sum_value, count(" + measure + ") AS count_value, " +
		"any(generation) AS generation, now64(9) AS built_at FROM " + c.tableIdentifier(plan.Request.Table) + " AS events " +
		"INNER JOIN (" + generationQuery + ") AS generations ON bucket_start = generations.fact_bucket_start " +
		"WHERE " + where + " GROUP BY bucket_start, dimension_hash"
	if _, err := c.execute(ctx, query, nil); err != nil {
		return fmt.Errorf("build aggregate fact values: %w", err)
	}
	markerQuery := "INSERT INTO " + c.database + "." + factBuildTable + " " +
		"SELECT " + quote(plan.Request.Table.TenantID) + ", " + quote(plan.Request.Table.TableID) + ", " +
		quote(plan.Request.Table.SchemaRevision) + ", " + quote(shapeKey) + ", fact_bucket_start, generation, now64(9) " +
		"FROM (" + generationQuery + ")"
	if _, err := c.execute(ctx, markerQuery, nil); err != nil {
		return fmt.Errorf("record aggregate fact bucket build: %w", err)
	}
	return nil
}

func (c clickHouseClient) currentFactGenerations(ctx context.Context, plan aggregateFactPlan, start, end time.Time) (map[time.Time]uint64, error) {
	query := c.factGenerationSubquery(plan.Request.Table, nil, start, end) + " FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	return decodeBucketGenerations(body, "fact_bucket_start")
}

func (c clickHouseClient) currentFactBuilds(ctx context.Context, plan aggregateFactPlan, start, end time.Time) (map[time.Time]uint64, error) {
	where := factIdentityWhere(plan.Request.Table) + " AND template_hash = " + quote(aggregateFactShapeKey(plan)) +
		" AND bucket_start >= toDateTime(" + quote(start.Format("2006-01-02 15:04:05")) + ", 'UTC')"
	if !end.IsZero() {
		where += " AND bucket_start < toDateTime(" + quote(end.Format("2006-01-02 15:04:05")) + ", 'UTC')"
	}
	query := "SELECT bucket_start AS fact_bucket_start, max(generation) AS generation FROM " + c.database + "." + factBuildTable +
		" WHERE " + where + " GROUP BY bucket_start FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	return decodeBucketGenerations(body, "fact_bucket_start")
}

func (c clickHouseClient) factGenerationSubquery(table TableContract, buckets []time.Time, start, end time.Time) string {
	where := factIdentityWhere(table)
	if len(buckets) > 0 {
		where += " AND bucket_start IN (" + clickHouseBucketList(buckets) + ")"
	} else if !start.IsZero() {
		where += " AND bucket_start >= toDateTime(" + quote(start.Format("2006-01-02 15:04:05")) + ", 'UTC')"
	}
	if len(buckets) == 0 && !end.IsZero() {
		where += " AND bucket_start < toDateTime(" + quote(end.Format("2006-01-02 15:04:05")) + ", 'UTC')"
	}
	return "SELECT bucket_start AS fact_bucket_start, sum(generation) AS generation FROM " + c.database + "." +
		factGenerationTable + " WHERE " + where + " GROUP BY bucket_start"
}

func (c clickHouseClient) readFactBuckets(ctx context.Context, plan aggregateFactPlan, generations map[time.Time]uint64, buckets []time.Time) (map[time.Time]aggregateBucketFact, error) {
	values := make([]string, len(plan.Dimensions))
	for i, dimension := range plan.Dimensions {
		field := plan.Request.Table.Fields[dimension.Field]
		values[i] = "toString(" + typedLiteral(dimension.Value, field.DataType) + ")"
	}
	dimensionHash := "hex(SHA256(toJSONString([" + strings.Join(values, ",") + "])))"
	where := factIdentityWhere(plan.Request.Table) + " AND template_hash = " + quote(aggregateFactShapeKey(plan)) +
		" AND dimension_hash = " + dimensionHash + " AND bucket_start IN (" + clickHouseBucketList(buckets) + ")"
	query := "SELECT bucket_start, generation, any(sum_value) AS sum_value, any(count_value) AS count_value FROM " +
		c.database + "." + factValueTable + " WHERE " + where + " GROUP BY bucket_start, generation FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[time.Time]aggregateBucketFact, len(buckets))
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		var row struct {
			BucketStart string  `json:"bucket_start"`
			Generation  uint64  `json:"generation"`
			SumValue    float64 `json:"sum_value"`
			CountValue  uint64  `json:"count_value"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, err
		}
		bucket, err := parseClickHouseBucket(row.BucketStart)
		if err != nil {
			return nil, err
		}
		if generations[bucket] == row.Generation {
			out[bucket] = aggregateBucketFact{Generation: row.Generation, Sum: row.SumValue, Count: row.CountValue}
		}
	}
	return out, scanner.Err()
}

func (c clickHouseClient) readRawFactComponents(ctx context.Context, plan aggregateFactPlan, filter *AggregateFilter) (aggregateFactComponents, error) {
	if filter == nil {
		return aggregateFactComponents{}, nil
	}
	filterSQL, err := buildFilterSQL(*filter, plan.Request.Table.Fields)
	if err != nil {
		return aggregateFactComponents{}, err
	}
	measure := quoteIdentifier(plan.Request.Field)
	sumExpr := "toFloat64(0)"
	if plan.NumericField {
		sumExpr = "coalesce(sum(toFloat64(" + measure + ")), 0)"
	}
	query := "SELECT " + sumExpr + " AS sum_value, count(" + measure + ") AS count_value FROM " +
		c.tableIdentifier(plan.Request.Table) + " WHERE " + filterSQL + " FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return aggregateFactComponents{}, err
	}
	var row struct {
		SumValue   float64 `json:"sum_value"`
		CountValue uint64  `json:"count_value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &row); err != nil {
		return aggregateFactComponents{}, err
	}
	return aggregateFactComponents{Sum: row.SumValue, Count: row.CountValue}, nil
}

func decodeBucketGenerations(body []byte, field string) (map[time.Time]uint64, error) {
	out := map[time.Time]uint64{}
	if field != "fact_bucket_start" {
		return nil, fmt.Errorf("unsupported fact bucket field %q", field)
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		var row struct {
			FactBucketStart string      `json:"fact_bucket_start"`
			Generation      json.Number `json:"generation"`
		}
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.UseNumber()
		if err := decoder.Decode(&row); err != nil {
			return nil, err
		}
		bucket, err := parseClickHouseBucket(row.FactBucketStart)
		if err != nil {
			return nil, err
		}
		generation, err := strconv.ParseUint(row.Generation.String(), 10, 64)
		if err != nil {
			return nil, err
		}
		out[bucket] = generation
	}
	return out, scanner.Err()
}

func parseClickHouseBucket(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid ClickHouse fact bucket %q", value)
}

func factIdentityWhere(table TableContract) string {
	return "tenant_id = " + quote(table.TenantID) + " AND table_id = " + quote(table.TableID) +
		" AND schema_revision = " + quote(table.SchemaRevision)
}

func clickHouseBucketList(buckets []time.Time) string {
	values := make([]string, len(buckets))
	for i, bucket := range buckets {
		values[i] = "toDateTime(" + quote(bucket.UTC().Format("2006-01-02 15:04:05")) + ", 'UTC')"
	}
	return strings.Join(values, ",")
}
