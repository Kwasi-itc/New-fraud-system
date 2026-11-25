package postgres

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
)

type RollupBucketRange struct {
	Resolution int32
	Start      time.Time // inclusive
	End        *time.Time // exclusive; nil means unbounded above
}

type RollupRawEdge struct {
	Lower          *time.Time
	LowerInclusive bool
	Upper          *time.Time
	UpperInclusive bool
}

type RollupWindowPlan struct {
	Buckets         []RollupBucketRange
	BucketSpanStart *time.Time // inclusive start of all bucketed spans
	BucketSpanEnd   *time.Time // exclusive end of all bucketed spans; nil if unbounded above
	RawEdges        []RollupRawEdge
}

// DecomposeRollupWindow splits [lower..upper] into disjoint UTC buckets (86400s, 3600s, 900s)
// within retention horizons, leaving at most two sub-15m raw edges.
func DecomposeRollupWindow(
	lower *time.Time, lowerInclusive bool,
	upper *time.Time, upperInclusive bool,
	now time.Time,
) (RollupWindowPlan, string) {
	if lower == nil {
		return RollupWindowPlan{}, "unbounded_lower"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	l := lower.UTC()
	if upper != nil {
		u := upper.UTC()
		if u.Before(l) || (u.Equal(l) && (!lowerInclusive || !upperInclusive)) {
			return RollupWindowPlan{}, "empty_window"
		}
	}

	// Retention horizon check: at least 900s (15m) or 3600s or 86400s must cover lower.
	eligible := make([]int32, 0, 3)
	for _, res := range []int32{ingestion.RollupResolution1d, ingestion.RollupResolution1h, ingestion.RollupResolution15m} {
		ret := ingestion.RollupRetentionForResolution(res)
		if !l.Before(now.UTC().Add(-ret)) {
			eligible = append(eligible, res)
		}
	}
	if len(eligible) == 0 {
		return RollupWindowPlan{}, "exceeds_retention"
	}

	// Minimum eligible resolution determines bucket boundary alignment.
	minRes := eligible[len(eligible)-1]
	firstBucket := firstWholeBucketStart(l, lowerInclusive, minRes)

	if upper == nil {
		// Unbounded above: buckets cover [firstBucket, +inf) with zero upper raw edge!
		var buckets []RollupBucketRange
		cursor := firstBucket
		// Step up to coarser buckets if aligned, then tail with coarsest eligible?
		// Actually, for unbounded above, if we align to largest eligible resolution (e.g. 1d),
		// [firstBucket, dayStart) uses 900/3600 and [dayStart, +inf) uses 1d!
		for idx := len(eligible) - 1; idx > 0; idx-- {
			res := eligible[idx]
			coarser := eligible[idx-1]
			nextCoarser := firstWholeBucketStart(cursor, true, coarser)
			if nextCoarser.After(cursor) {
				endCopy := nextCoarser
				buckets = append(buckets, RollupBucketRange{
					Resolution: res,
					Start:      cursor,
					End:        &endCopy,
				})
				cursor = nextCoarser
			}
		}
		buckets = append(buckets, RollupBucketRange{
			Resolution: eligible[0],
			Start:      cursor,
			End:        nil,
		})
		spanStart := firstBucket
		var rawEdges []RollupRawEdge
		if firstBucket.After(l) || (firstBucket.Equal(l) && !lowerInclusive) {
			lCopy := l
			fbCopy := firstBucket
			rawEdges = append(rawEdges, RollupRawEdge{
				Lower:          &lCopy,
				LowerInclusive: lowerInclusive,
				Upper:          &fbCopy,
				UpperInclusive: false,
			})
		}
		return RollupWindowPlan{
			Buckets:         buckets,
			BucketSpanStart: &spanStart,
			BucketSpanEnd:   nil,
			RawEdges:        rawEdges,
		}, ""
	}

	u := upper.UTC()
	lastBucketEnd := lastWholeBucketEnd(u, upperInclusive, minRes)
	if !lastBucketEnd.After(firstBucket) {
		// Window is narrower than a single bucket; use raw edge only (or 1 raw query).
		return RollupWindowPlan{}, "window_smaller_than_bucket"
	}

	// Greedy hierarchical decomposition inside [firstBucket, lastBucketEnd)
	var buckets []RollupBucketRange
	left := firstBucket
	right := lastBucketEnd

	for _, res := range eligible {
		if !right.After(left) {
			break
		}
		cStart := firstWholeBucketStart(left, true, res)
		cEnd := lastWholeBucketEnd(right, true, res)
		if !cEnd.After(cStart) {
			continue
		}
		// Add [cStart, cEnd) at resolution res, and recurse/continue on [left, cStart) and [cEnd, right)
		// Since eligible is ordered 86400 -> 3600 -> 900, cStart..cEnd is a single contiguous middle span!
		buckets = append(buckets, RollupBucketRange{
			Resolution: res,
			Start:      cStart,
			End:        &cEnd,
		})
		// Emit left and right flanking sub-ranges using finer resolutions.
		if cStart.After(left) {
			buckets = append(buckets, decomposeAlignedSpan(left, cStart, eligible)...)
		}
		if right.After(cEnd) {
			buckets = append(buckets, decomposeAlignedSpan(cEnd, right, eligible)...)
		}
		left = right
		break
	}

	var rawEdges []RollupRawEdge
	if firstBucket.After(l) || (firstBucket.Equal(l) && !lowerInclusive) {
		lCopy := l
		fbCopy := firstBucket
		rawEdges = append(rawEdges, RollupRawEdge{
			Lower:          &lCopy,
			LowerInclusive: lowerInclusive,
			Upper:          &fbCopy,
			UpperInclusive: false,
		})
	}
	if u.After(lastBucketEnd) || (u.Equal(lastBucketEnd) && upperInclusive) {
		lbCopy := lastBucketEnd
		uCopy := u
		rawEdges = append(rawEdges, RollupRawEdge{
			Lower:          &lbCopy,
			LowerInclusive: true,
			Upper:          &uCopy,
			UpperInclusive: upperInclusive,
		})
	}

	spanStart := firstBucket
	spanEnd := lastBucketEnd
	return RollupWindowPlan{
		Buckets:         buckets,
		BucketSpanStart: &spanStart,
		BucketSpanEnd:   &spanEnd,
		RawEdges:        rawEdges,
	}, ""
}

func decomposeAlignedSpan(start, end time.Time, eligible []int32) []RollupBucketRange {
	var out []RollupBucketRange
	cur := start
	for _, res := range eligible {
		if !end.After(cur) {
			break
		}
		cStart := firstWholeBucketStart(cur, true, res)
		cEnd := lastWholeBucketEnd(end, true, res)
		if !cEnd.After(cStart) {
			continue
		}
		if cStart.After(cur) {
			out = append(out, decomposeAlignedSpan(cur, cStart, eligible)...)
		}
		out = append(out, RollupBucketRange{
			Resolution: res,
			Start:      cStart,
			End:        &cEnd,
		})
		if end.After(cEnd) {
			out = append(out, decomposeAlignedSpan(cEnd, end, eligible)...)
		}
		return out
	}
	return out
}

func firstWholeBucketStart(t time.Time, inclusive bool, res int32) time.Time {
	aligned := ingestion.AlignRollupBucketUTC(t, res)
	if aligned.Equal(t) && inclusive {
		return aligned
	}
	return aligned.Add(time.Duration(res) * time.Second)
}

func lastWholeBucketEnd(t time.Time, inclusive bool, res int32) time.Time {
	aligned := ingestion.AlignRollupBucketUTC(t, res)
	if aligned.Equal(t) && !inclusive {
		return aligned.Add(-time.Duration(res) * time.Second)
	}
	return aligned
}

type RollupQueryCandidate struct {
	Definition ingestion.RollupDefinition
	GroupValue string
	Plan       RollupWindowPlan
	Aggregate  string
}

func checkRollupEligibility(
	model ingestion.PublishedDataModel,
	table ingestion.ObjectSchema,
	query ingestion.AggregateQuery,
	defs []ingestion.RollupDefinition,
	now time.Time,
) (RollupQueryCandidate, string) {
	agg := strings.ToLower(strings.TrimSpace(query.Aggregate))
	switch agg {
	case "count", "sum", "avg":
	default:
		return RollupQueryCandidate{}, "unsupported_aggregate"
	}

	if query.Filter == nil {
		return RollupQueryCandidate{}, "missing_filter"
	}
	preds, ok := flattenAndFilter(*query.Filter)
	if !ok {
		return RollupQueryCandidate{}, "non_conjunctive_filter"
	}

	var groupField, groupValue string
	var timeField string
	var lower *time.Time
	var lowerIncl bool
	var upper *time.Time
	var upperIncl bool

	for _, p := range preds {
		op := strings.ToLower(strings.TrimSpace(p.Op))
		switch op {
		case "eq", "=":
			if groupField != "" {
				return RollupQueryCandidate{}, "multiple_eq_predicates"
			}
			groupField = p.Field
			if p.Value == nil {
				return RollupQueryCandidate{}, "null_group_value"
			}
			groupValue = fmt.Sprint(p.Value)
		case "gt", ">", "gte", ">=":
			if timeField != "" && timeField != p.Field {
				return RollupQueryCandidate{}, "multiple_time_fields"
			}
			timeField = p.Field
			ts, ok := coerceTimestampValue(p.Value)
			if !ok {
				return RollupQueryCandidate{}, "non_timestamp_bound"
			}
			incl := op == "gte" || op == ">="
			if lower == nil || ts.After(*lower) || (ts.Equal(*lower) && !incl) {
				lower = &ts
				lowerIncl = incl
			}
		case "lt", "<", "lte", "<=":
			if timeField != "" && timeField != p.Field {
				return RollupQueryCandidate{}, "multiple_time_fields"
			}
			timeField = p.Field
			ts, ok := coerceTimestampValue(p.Value)
			if !ok {
				return RollupQueryCandidate{}, "non_timestamp_bound"
			}
			incl := op == "lte" || op == "<="
			if upper == nil || ts.Before(*upper) || (ts.Equal(*upper) && !incl) {
				upper = &ts
				upperIncl = incl
			}
		default:
			return RollupQueryCandidate{}, "unsupported_operator"
		}
	}

	if groupField == "" || timeField == "" {
		return RollupQueryCandidate{}, "missing_group_or_time_predicate"
	}
	timeSchema, ok := table.Fields[timeField]
	if !ok || !isTimestampDataType(timeSchema.DataType) {
		return RollupQueryCandidate{}, "non_timestamptz_field"
	}

	var matched *ingestion.RollupDefinition
	for i := range defs {
		d := &defs[i]
		if d.TableName == table.Name && d.GroupField == groupField && d.TimeField == timeField && d.ValueField == query.Field {
			matched = d
			break
		}
	}
	if matched == nil {
		return RollupQueryCandidate{}, "no_matching_definition"
	}
	if matched.Status != ingestion.RollupStatusReady {
		return RollupQueryCandidate{}, "definition_not_ready"
	}
	if matched.ModelVersion != model.RevisionID {
		return RollupQueryCandidate{}, "model_version_mismatch"
	}

	plan, reason := DecomposeRollupWindow(lower, lowerIncl, upper, upperIncl, now)
	if reason != "" {
		return RollupQueryCandidate{}, reason
	}

	return RollupQueryCandidate{
		Definition: *matched,
		GroupValue: groupValue,
		Plan:       plan,
		Aggregate:  agg,
	}, ""
}

func flattenAndFilter(f ingestion.AggregateFilter) ([]ingestion.AggregateFilter, bool) {
	switch strings.ToLower(strings.TrimSpace(f.Kind)) {
	case "", ingestion.AggregateFilterKindGroup:
		op := strings.ToLower(strings.TrimSpace(f.Operator))
		if op != "" && op != "and" {
			return nil, false
		}
		var out []ingestion.AggregateFilter
		for _, child := range f.Children {
			sub, ok := flattenAndFilter(child)
			if !ok {
				return nil, false
			}
			out = append(out, sub...)
		}
		return out, true
	case ingestion.AggregateFilterKindPredicate:
		return []ingestion.AggregateFilter{f}, true
	default:
		return nil, false
	}
}

func isTimestampDataType(dt string) bool {
	s := strings.ToLower(strings.TrimSpace(dt))
	return strings.Contains(s, "time") || s == "date"
}

func coerceTimestampValue(v any) (time.Time, bool) {
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

func buildRollupUnionSQL(
	model ingestion.PublishedDataModel,
	table ingestion.ObjectSchema,
	cand RollupQueryCandidate,
) (string, []any, error) {
	schemaName := schemaNameForModel(model)
	qualifiedTable := sanitizeIdentifier(schemaName, table.Name)
	groupCol := sanitizeIdentifier(cand.Definition.GroupField)
	timeCol := sanitizeIdentifier(cand.Definition.TimeField)
	valCol := sanitizeIdentifier(cand.Definition.ValueField)

	args := []any{
		model.TenantID.String(),
		cand.Definition.ID,
		cand.GroupValue,
		cand.Definition.BackfillSnapshot,
	}

	// Branch 1: rollup_buckets
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
		WHERE tenant_id = $1 AND definition_id = $2 AND group_value = $3
			AND (%s)
	`, strings.Join(bucketConds, " OR "))

	// Branch 2: rollup_deltas (unapplied deltas inside [BucketSpanStart, BucketSpanEnd))
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
		WHERE tenant_id = $1 AND definition_id = $2 AND group_value = $3
			AND (%s)
			AND ($4::pg_snapshot IS NULL OR NOT pg_visible_in_snapshot(xid, $4::pg_snapshot))
	`, deltaTimeSQL)

	branches := []string{branch1, branch2}

	// Branch 3: raw edges
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
		if cand.Definition.ValueType == ingestion.RollupValueTypeInt || cand.Definition.ValueType == ingestion.RollupValueTypeFloat {
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
		if cand.Definition.ValueType == ingestion.RollupValueTypeInt {
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

func executeRollupQuery(
	ctx context.Context,
	db txExecutor,
	statementTimeout time.Duration,
	sql string,
	args []any,
) (any, error) {
	var value any
	var queryErr error
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
			queryErr = tx.QueryRow(ctx, sql, args...).Scan(&value)
			if queryErr == nil {
				queryErr = tx.Commit(ctx)
			}
			return value, queryErr
		}
	}
	queryErr = db.QueryRow(ctx, sql, args...).Scan(&value)
	return value, queryErr
}

func compareRollupShadowValues(agg string, rawVal, rollupVal any) bool {
	if rawVal == nil && rollupVal == nil {
		return true
	}
	if rawVal == nil || rollupVal == nil {
		return false
	}
	if agg == "count" {
		return fmt.Sprint(rawVal) == fmt.Sprint(rollupVal)
	}
	rf, ok1 := asFloat64Shadow(rawVal)
	qf, ok2 := asFloat64Shadow(rollupVal)
	if !ok1 || !ok2 {
		return fmt.Sprint(rawVal) == fmt.Sprint(rollupVal)
	}
	if rf == qf {
		return true
	}
	denom := math.Max(1.0, math.Abs(rf))
	return math.Abs(rf-qf)/denom <= 1e-9
}

func asFloat64Shadow(v any) (float64, bool) {
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
