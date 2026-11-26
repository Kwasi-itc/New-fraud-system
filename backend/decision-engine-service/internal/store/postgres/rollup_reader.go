package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/jackc/pgx/v5"
)

const (
	directDBRollupRes15m int32 = 900
	directDBRollupRes1h  int32 = 3600
	directDBRollupRes1d  int32 = 86400

	directDBRollupRet15m = 14 * 24 * time.Hour
	directDBRollupRet1h  = 90 * 24 * time.Hour
	directDBRollupRet1d  = 3 * 365 * 24 * time.Hour
)

type directDBRollupDef struct {
	ID               string
	TableName        string
	GroupField       string
	TimeField        string
	ValueField       string
	ValueType        string
	ModelVersion     string
	Status           string
	BackfillSnapshot *string
}

type directDBRollupCacheKey struct {
	tenantID  string
	tableName string
}

type directDBRollupCacheEntry struct {
	defs      []directDBRollupDef
	expiresAt time.Time
}

// DirectDBRollupCache caches read-only ready rollup definitions for decision-engine direct_db mode.
type DirectDBRollupCache struct {
	db      txExecutor
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[directDBRollupCacheKey]directDBRollupCacheEntry
}

func NewDirectDBRollupCache(db txExecutor, ttl time.Duration) *DirectDBRollupCache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &DirectDBRollupCache{
		db:      db,
		ttl:     ttl,
		entries: make(map[directDBRollupCacheKey]directDBRollupCacheEntry),
	}
}

func (c *DirectDBRollupCache) GetReadyForTable(ctx context.Context, tenantID, tableName string) ([]directDBRollupDef, error) {
	if c == nil || c.db == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	key := directDBRollupCacheKey{tenantID: tenantID, tableName: tableName}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.defs, nil
	}

	const stmt = `
		SELECT id::text, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1 AND table_name = $2 AND status = 'ready'
		ORDER BY created_at
	`
	rows, err := c.db.Query(ctx, stmt, tenantID, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []directDBRollupDef
	for rows.Next() {
		var d directDBRollupDef
		if err := rows.Scan(
			&d.ID, &d.TableName, &d.GroupField, &d.TimeField, &d.ValueField,
			&d.ValueType, &d.ModelVersion, &d.Status, &d.BackfillSnapshot,
		); err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[key] = directDBRollupCacheEntry{
		defs:      defs,
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
	return defs, nil
}

type directDBRollupBucketRange struct {
	Resolution int32
	Start      time.Time
	End        *time.Time
}

type directDBRollupRawEdge struct {
	Lower          *time.Time
	LowerInclusive bool
	Upper          *time.Time
	UpperInclusive bool
}

type directDBRollupWindowPlan struct {
	Buckets         []directDBRollupBucketRange
	BucketSpanStart *time.Time
	BucketSpanEnd   *time.Time
	RawEdges        []directDBRollupRawEdge
}

func directDBRetentionForResolution(res int32) time.Duration {
	switch res {
	case directDBRollupRes15m:
		return directDBRollupRet15m
	case directDBRollupRes1h:
		return directDBRollupRet1h
	case directDBRollupRes1d:
		return directDBRollupRet1d
	default:
		return 0
	}
}

func alignDirectDBBucketUTC(t time.Time, res int32) time.Time {
	u := t.UTC()
	sec := int64(res)
	if sec <= 0 {
		return u
	}
	unix := u.Unix()
	aligned := unix - ((unix%sec + sec) % sec)
	return time.Unix(aligned, 0).UTC()
}

func firstDirectDBWholeBucketStart(t time.Time, inclusive bool, res int32) time.Time {
	aligned := alignDirectDBBucketUTC(t, res)
	if aligned.Equal(t) && inclusive {
		return aligned
	}
	return aligned.Add(time.Duration(res) * time.Second)
}

func lastDirectDBWholeBucketEnd(t time.Time, inclusive bool, res int32) time.Time {
	aligned := alignDirectDBBucketUTC(t, res)
	if aligned.Equal(t) && !inclusive {
		return aligned.Add(-time.Duration(res) * time.Second)
	}
	return aligned
}

func decomposeDirectDBRollupWindow(
	lower *time.Time, lowerInclusive bool,
	upper *time.Time, upperInclusive bool,
	now time.Time,
) (directDBRollupWindowPlan, string) {
	if lower == nil {
		return directDBRollupWindowPlan{}, "unbounded_lower"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	l := lower.UTC()
	if upper != nil {
		u := upper.UTC()
		if u.Before(l) || (u.Equal(l) && (!lowerInclusive || !upperInclusive)) {
			return directDBRollupWindowPlan{}, "empty_window"
		}
	}

	eligible := make([]int32, 0, 3)
	for _, res := range []int32{directDBRollupRes1d, directDBRollupRes1h, directDBRollupRes15m} {
		ret := directDBRetentionForResolution(res)
		if !l.Before(now.UTC().Add(-ret)) {
			eligible = append(eligible, res)
		}
	}
	if len(eligible) == 0 {
		return directDBRollupWindowPlan{}, "exceeds_retention"
	}

	minRes := eligible[len(eligible)-1]
	firstBucket := firstDirectDBWholeBucketStart(l, lowerInclusive, minRes)

	if upper == nil {
		var buckets []directDBRollupBucketRange
		cursor := firstBucket
		for idx := len(eligible) - 1; idx > 0; idx-- {
			res := eligible[idx]
			coarser := eligible[idx-1]
			nextCoarser := firstDirectDBWholeBucketStart(cursor, true, coarser)
			if nextCoarser.After(cursor) {
				endCopy := nextCoarser
				buckets = append(buckets, directDBRollupBucketRange{
					Resolution: res,
					Start:      cursor,
					End:        &endCopy,
				})
				cursor = nextCoarser
			}
		}
		buckets = append(buckets, directDBRollupBucketRange{
			Resolution: eligible[0],
			Start:      cursor,
			End:        nil,
		})
		spanStart := firstBucket
		var rawEdges []directDBRollupRawEdge
		if firstBucket.After(l) || (firstBucket.Equal(l) && !lowerInclusive) {
			lCopy := l
			fbCopy := firstBucket
			rawEdges = append(rawEdges, directDBRollupRawEdge{
				Lower:          &lCopy,
				LowerInclusive: lowerInclusive,
				Upper:          &fbCopy,
				UpperInclusive: false,
			})
		}
		return directDBRollupWindowPlan{
			Buckets:         buckets,
			BucketSpanStart: &spanStart,
			BucketSpanEnd:   nil,
			RawEdges:        rawEdges,
		}, ""
	}

	u := upper.UTC()
	lastBucketEnd := lastDirectDBWholeBucketEnd(u, upperInclusive, minRes)
	if !lastBucketEnd.After(firstBucket) {
		return directDBRollupWindowPlan{}, "window_smaller_than_bucket"
	}

	var buckets []directDBRollupBucketRange
	left := firstBucket
	right := lastBucketEnd
	for _, res := range eligible {
		if !right.After(left) {
			break
		}
		cStart := firstDirectDBWholeBucketStart(left, true, res)
		cEnd := lastDirectDBWholeBucketEnd(right, true, res)
		if !cEnd.After(cStart) {
			continue
		}
		buckets = append(buckets, directDBRollupBucketRange{
			Resolution: res,
			Start:      cStart,
			End:        &cEnd,
		})
		if cStart.After(left) {
			buckets = append(buckets, decomposeDirectDBAlignedSpan(left, cStart, eligible)...)
		}
		if right.After(cEnd) {
			buckets = append(buckets, decomposeDirectDBAlignedSpan(cEnd, right, eligible)...)
		}
		break
	}

	var rawEdges []directDBRollupRawEdge
	if firstBucket.After(l) || (firstBucket.Equal(l) && !lowerInclusive) {
		lCopy := l
		fbCopy := firstBucket
		rawEdges = append(rawEdges, directDBRollupRawEdge{
			Lower:          &lCopy,
			LowerInclusive: lowerInclusive,
			Upper:          &fbCopy,
			UpperInclusive: false,
		})
	}
	if u.After(lastBucketEnd) || (u.Equal(lastBucketEnd) && upperInclusive) {
		lbCopy := lastBucketEnd
		uCopy := u
		rawEdges = append(rawEdges, directDBRollupRawEdge{
			Lower:          &lbCopy,
			LowerInclusive: true,
			Upper:          &uCopy,
			UpperInclusive: upperInclusive,
		})
	}

	spanStart := firstBucket
	spanEnd := lastBucketEnd
	return directDBRollupWindowPlan{
		Buckets:         buckets,
		BucketSpanStart: &spanStart,
		BucketSpanEnd:   &spanEnd,
		RawEdges:        rawEdges,
	}, ""
}

func decomposeDirectDBAlignedSpan(start, end time.Time, eligible []int32) []directDBRollupBucketRange {
	var out []directDBRollupBucketRange
	cur := start
	for _, res := range eligible {
		if !end.After(cur) {
			break
		}
		cStart := firstDirectDBWholeBucketStart(cur, true, res)
		cEnd := lastDirectDBWholeBucketEnd(end, true, res)
		if !cEnd.After(cStart) {
			continue
		}
		if cStart.After(cur) {
			out = append(out, decomposeDirectDBAlignedSpan(cur, cStart, eligible)...)
		}
		out = append(out, directDBRollupBucketRange{
			Resolution: res,
			Start:      cStart,
			End:        &cEnd,
		})
		if end.After(cEnd) {
			out = append(out, decomposeDirectDBAlignedSpan(cEnd, end, eligible)...)
		}
		return out
	}
	return out
}

type directDBRollupCandidate struct {
	Definition directDBRollupDef
	GroupValue string
	Plan       directDBRollupWindowPlan
	Aggregate  string
}

func checkDirectDBRollupEligibility(
	model ports.TenantModel,
	table ports.TenantModelTable,
	query ports.AggregateQuery,
	defs []directDBRollupDef,
	now time.Time,
) (directDBRollupCandidate, string) {
	agg := strings.ToLower(strings.TrimSpace(query.Aggregate))
	switch agg {
	case "count", "sum", "avg":
	default:
		return directDBRollupCandidate{}, "unsupported_aggregate"
	}
	if query.Filter == nil {
		return directDBRollupCandidate{}, "missing_filter"
	}
	preds, ok := flattenDirectDBAndFilter(*query.Filter)
	if !ok {
		return directDBRollupCandidate{}, "non_conjunctive_filter"
	}

	var groupField, groupValue, timeField string
	var lower *time.Time
	var lowerIncl bool
	var upper *time.Time
	var upperIncl bool

	for _, p := range preds {
		op := strings.ToLower(strings.TrimSpace(p.Op))
		switch op {
		case "eq", "=":
			if groupField != "" {
				return directDBRollupCandidate{}, "multiple_eq_predicates"
			}
			groupField = p.Field
			if p.Value == nil {
				return directDBRollupCandidate{}, "null_group_value"
			}
			groupValue = fmt.Sprint(p.Value)
		case "gt", ">", "gte", ">=":
			if timeField != "" && timeField != p.Field {
				return directDBRollupCandidate{}, "multiple_time_fields"
			}
			timeField = p.Field
			ts, ok := coerceDirectDBTimestampValue(p.Value)
			if !ok {
				return directDBRollupCandidate{}, "non_timestamp_bound"
			}
			incl := op == "gte" || op == ">="
			if lower == nil || ts.After(*lower) || (ts.Equal(*lower) && !incl) {
				lower = &ts
				lowerIncl = incl
			}
		case "lt", "<", "lte", "<=":
			if timeField != "" && timeField != p.Field {
				return directDBRollupCandidate{}, "multiple_time_fields"
			}
			timeField = p.Field
			ts, ok := coerceDirectDBTimestampValue(p.Value)
			if !ok {
				return directDBRollupCandidate{}, "non_timestamp_bound"
			}
			incl := op == "lte" || op == "<="
			if upper == nil || ts.Before(*upper) || (ts.Equal(*upper) && !incl) {
				upper = &ts
				upperIncl = incl
			}
		default:
			return directDBRollupCandidate{}, "unsupported_operator"
		}
	}

	if groupField == "" || timeField == "" {
		return directDBRollupCandidate{}, "missing_group_or_time_predicate"
	}
	timeSchema, ok := table.Fields[timeField]
	if !ok || !isDirectDBTimestampType(timeSchema.Type) {
		return directDBRollupCandidate{}, "non_timestamptz_field"
	}

	var matched *directDBRollupDef
	for i := range defs {
		d := &defs[i]
		if d.TableName == table.Name && d.GroupField == groupField && d.TimeField == timeField && d.ValueField == query.Field {
			matched = d
			break
		}
	}
	if matched == nil {
		return directDBRollupCandidate{}, "no_matching_definition"
	}
	if matched.Status != "ready" {
		return directDBRollupCandidate{}, "definition_not_ready"
	}
	if matched.ModelVersion != model.RevisionID {
		return directDBRollupCandidate{}, "model_version_mismatch"
	}

	plan, reason := decomposeDirectDBRollupWindow(lower, lowerIncl, upper, upperIncl, now)
	if reason != "" {
		return directDBRollupCandidate{}, reason
	}
	return directDBRollupCandidate{
		Definition: *matched,
		GroupValue: groupValue,
		Plan:       plan,
		Aggregate:  agg,
	}, ""
}

func flattenDirectDBAndFilter(f ports.AggregateFilter) ([]ports.AggregateFilter, bool) {
	switch strings.ToLower(strings.TrimSpace(f.Kind)) {
	case "", "group":
		op := strings.ToLower(strings.TrimSpace(f.Operator))
		if op != "" && op != "and" {
			return nil, false
		}
		var out []ports.AggregateFilter
		for _, child := range f.Children {
			sub, ok := flattenDirectDBAndFilter(child)
			if !ok {
				return nil, false
			}
			out = append(out, sub...)
		}
		return out, true
	case "predicate":
		return []ports.AggregateFilter{f}, true
	default:
		return nil, false
	}
}

func isDirectDBTimestampType(dt string) bool {
	s := strings.ToLower(strings.TrimSpace(dt))
	return strings.Contains(s, "time") || s == "date"
}

func coerceDirectDBTimestampValue(v any) (time.Time, bool) {
	switch typed := v.(type) {
	case time.Time:
		return typed.UTC(), true
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(typed)); err == nil {
				return parsed.UTC(), true
			}
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func buildDirectDBRollupUnionSQL(
	tenantID, schemaName string,
	table ports.TenantModelTable,
	cand directDBRollupCandidate,
) (string, []any, error) {
	qualifiedTable := sanitizeIdentifier(schemaName, table.Name)
	groupCol := sanitizeIdentifier(cand.Definition.GroupField)
	timeCol := sanitizeIdentifier(cand.Definition.TimeField)
	valCol := sanitizeIdentifier(cand.Definition.ValueField)

	args := []any{
		tenantID,
		cand.Definition.ID,
		cand.GroupValue,
		cand.Definition.BackfillSnapshot,
	}

	var bucketConds []string
	for _, b := range cand.Plan.Buckets {
		resIdx := len(args) + 1
		startIdx := len(args) + 2
		args = append(args, b.Resolution, b.Start.UTC())
		if b.End != nil {
			endIdx := len(args) + 1
			args = append(args, b.End.UTC())
			bucketConds = append(bucketConds, fmt.Sprintf("(resolution = $%d AND bucket_start >= $%d AND bucket_start < $%d)", resIdx, startIdx, endIdx))
		} else {
			bucketConds = append(bucketConds, fmt.Sprintf("(resolution = $%d AND bucket_start >= $%d)", resIdx, startIdx))
		}
	}
	branch1 := fmt.Sprintf(`
		SELECT rows AS c, value_count AS vc, value_sum AS s
		FROM core_ingestion.rollup_buckets
		WHERE tenant_id = $1::uuid AND definition_id = $2::uuid AND group_value = $3
			AND (%s)
	`, strings.Join(bucketConds, " OR "))

	var deltaTimeConds []string
	if cand.Plan.BucketSpanStart != nil {
		idx := len(args) + 1
		args = append(args, cand.Plan.BucketSpanStart.UTC())
		deltaTimeConds = append(deltaTimeConds, fmt.Sprintf("event_time >= $%d", idx))
	}
	if cand.Plan.BucketSpanEnd != nil {
		idx := len(args) + 1
		args = append(args, cand.Plan.BucketSpanEnd.UTC())
		deltaTimeConds = append(deltaTimeConds, fmt.Sprintf("event_time < $%d", idx))
	}
	deltaTimeSQL := "TRUE"
	if len(deltaTimeConds) > 0 {
		deltaTimeSQL = strings.Join(deltaTimeConds, " AND ")
	}
	branch2 := fmt.Sprintf(`
		SELECT d_rows AS c, d_value_count AS vc, d_value_sum AS s
		FROM core_ingestion.rollup_deltas
		WHERE tenant_id = $1::uuid AND definition_id = $2::uuid AND group_value = $3
			AND (%s)
			AND ($4::pg_snapshot IS NULL OR NOT pg_visible_in_snapshot(xid, $4::pg_snapshot))
	`, deltaTimeSQL)

	branches := []string{branch1, branch2}
	if len(cand.Plan.RawEdges) > 0 {
		var edgeConds []string
		for _, edge := range cand.Plan.RawEdges {
			var parts []string
			if edge.Lower != nil {
				op := ">"
				if edge.LowerInclusive {
					op = ">="
				}
				idx := len(args) + 1
				args = append(args, edge.Lower.UTC())
				parts = append(parts, fmt.Sprintf("%s %s $%d", timeCol, op, idx))
			}
			if edge.Upper != nil {
				op := "<"
				if edge.UpperInclusive {
					op = "<="
				}
				idx := len(args) + 1
				args = append(args, edge.Upper.UTC())
				parts = append(parts, fmt.Sprintf("%s %s $%d", timeCol, op, idx))
			}
			edgeConds = append(edgeConds, "("+strings.Join(parts, " AND ")+")")
		}
		valSumExpr := "0::numeric"
		if cand.Definition.ValueType == "int" || cand.Definition.ValueType == "float" {
			valSumExpr = fmt.Sprintf("COALESCE(%s::numeric, 0)", valCol)
		}
		branch3 := fmt.Sprintf(`
			SELECT 1::bigint AS c,
				CASE WHEN %s IS NULL THEN 0::bigint ELSE 1::bigint END AS vc,
				%s AS s
			FROM %s
			WHERE %s::text = $3 AND (%s)
		`, valCol, valSumExpr, qualifiedTable, groupCol, strings.Join(edgeConds, " OR "))
		branches = append(branches, branch3)
	}

	var finalSelect string
	switch cand.Aggregate {
	case "count":
		finalSelect = "COALESCE(SUM(vc), 0)::bigint"
	case "sum":
		if cand.Definition.ValueType == "int" {
			finalSelect = "COALESCE(SUM(s), 0)::bigint"
		} else {
			finalSelect = "COALESCE(SUM(s), 0)::double precision"
		}
	case "avg":
		finalSelect = "(SUM(s) / NULLIF(SUM(vc), 0))::double precision"
	default:
		return "", nil, fmt.Errorf("unsupported rollup aggregate %q", cand.Aggregate)
	}

	sql := fmt.Sprintf("WITH combined AS (\n%s\n)\nSELECT %s FROM combined", strings.Join(branches, "\nUNION ALL\n"), finalSelect)
	return sql, args, nil
}

func executeDirectDBQueryWithTimeout(
	ctx context.Context,
	db txExecutor,
	statementTimeout time.Duration,
	sql string,
	args []any,
) (any, error) {
	var value any
	if statementTimeout > 0 {
		if beginner, ok := db.(readOnlyTxBeginner); ok {
			tx, txErr := beginner.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if txErr != nil {
				return nil, txErr
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, execErr := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", max(1, int(statementTimeout.Milliseconds())))); execErr != nil {
				return nil, execErr
			}
			if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
				return nil, err
			}
			return value, tx.Commit(ctx)
		}
	}
	if err := db.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func compareDirectDBRollupShadow(agg string, rawVal, rollupVal any) bool {
	if rawVal == nil && rollupVal == nil {
		return true
	}
	if rawVal == nil || rollupVal == nil {
		return false
	}
	if agg == "count" {
		return fmt.Sprint(rawVal) == fmt.Sprint(rollupVal)
	}
	rf, ok1 := asDirectDBFloat64(rawVal)
	qf, ok2 := asDirectDBFloat64(rollupVal)
	if !ok1 || !ok2 {
		return fmt.Sprint(rawVal) == fmt.Sprint(rollupVal)
	}
	if rf == qf {
		return true
	}
	denom := math.Max(1.0, math.Abs(rf))
	return math.Abs(rf-qf)/denom <= 1e-9
}

func asDirectDBFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case int:
		return float64(t), true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	default:
		return 0, false
	}
}

func logDirectDBRollupShadowMismatch(tenantID, defID, agg string, rawVal, rollupVal any) {
	slog.Default().Warn("direct_db rollup shadow mismatch",
		"tenant_id", tenantID,
		"definition_id", defID,
		"aggregate", agg,
		"raw_value", rawVal,
		"rollup_value", rollupVal,
	)
}
