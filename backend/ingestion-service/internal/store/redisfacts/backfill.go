package redisfacts

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

const backfillPipelineSize = 500

// Status reports whether every active aggregate fact has complete historical
// coverage and whether there are committed PostgreSQL writes whose fact update
// still needs reconciliation.
func (w *Writer) Status(ctx context.Context, tenantID uuid.UUID) (ports.AggregateFactBackfillStatus, error) {
	definitions, err := w.listActiveForTenant(ctx, tenantID.String())
	if err != nil {
		return ports.AggregateFactBackfillStatus{}, fmt.Errorf("load aggregate fact registry: %w", err)
	}
	return w.statusForDefinitions(ctx, tenantID, definitions)
}

func (w *Writer) statusForDefinitions(ctx context.Context, tenantID uuid.UUID, definitions []ports.AggregateFactDefinition) (ports.AggregateFactBackfillStatus, error) {
	status := ports.AggregateFactBackfillStatus{
		TenantID:        tenantID.String(),
		DefinitionCount: len(definitions),
		Definitions:     make([]ports.AggregateFactDefinitionStatus, 0, len(definitions)),
	}
	if err := w.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM core_ingestion.idempotency_keys
		WHERE tenant_id = $1 AND fact_status = 'pending'
	`, tenantID).Scan(&status.PendingReceipts); err != nil {
		return ports.AggregateFactBackfillStatus{}, fmt.Errorf("count pending aggregate fact receipts: %w", err)
	}

	pipe := w.client.Pipeline()
	coverageCommands := make([]*redis.StringCmd, len(definitions))
	dirtyCommands := make([]*redis.IntCmd, len(definitions))
	for index, definition := range definitions {
		coverageCommands[index] = pipe.Get(ctx, factCoverageKey(tenantID.String(), definition))
		dirtyCommands[index] = pipe.Exists(ctx, factDirtyKey(tenantID.String(), definition))
	}
	if len(definitions) > 0 {
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return ports.AggregateFactBackfillStatus{}, fmt.Errorf("read aggregate fact readiness: %w", err)
		}
	}

	ready := status.PendingReceipts == 0
	for index, definition := range definitions {
		coverage, coverageErr := coverageCommands[index].Result()
		coverageReady := coverageErr == nil && coverage == definition.CoverageToken+"|all"
		dirty, dirtyErr := dirtyCommands[index].Result()
		if dirtyErr != nil {
			return ports.AggregateFactBackfillStatus{}, fmt.Errorf("read aggregate fact dirty marker: %w", dirtyErr)
		}
		definitionStatus := ports.AggregateFactDefinitionStatus{
			ID:               definition.ID,
			TableName:        definition.TableName,
			Signature:        definition.Signature,
			Version:          definition.Version,
			BackfillRequired: definition.BackfillRequired,
			CoverageReady:    coverageReady,
			Dirty:            dirty > 0,
		}
		status.Definitions = append(status.Definitions, definitionStatus)
		if definition.BackfillRequired || !coverageReady || dirty > 0 {
			ready = false
		}
	}
	status.Ready = ready
	return status, nil
}

// Backfill completely rebuilds the active tenant fact generations from the
// authoritative PostgreSQL event tables. It is intentionally blocking: normal
// writes in this service finish first, then new writes wait until coverage is
// complete and the dirty markers have been removed.
func (w *Writer) Backfill(ctx context.Context, tenantID uuid.UUID, force bool) (result ports.AggregateFactBackfillResult, resultErr error) {
	startedAt := time.Now().UTC()
	result = ports.AggregateFactBackfillResult{
		TenantID: tenantID.String(), StartedAt: startedAt,
		DefinitionResults: make([]ports.AggregateFactDefinitionBackfillResult, 0),
	}

	w.maintenance.Lock()
	defer w.maintenance.Unlock()

	definitions, err := w.listActiveForTenant(ctx, tenantID.String())
	if err != nil {
		return result, fmt.Errorf("load aggregate fact registry: %w", err)
	}
	before, err := w.statusForDefinitions(ctx, tenantID, definitions)
	if err != nil {
		return result, err
	}
	if before.Ready && !force {
		result.Status = before
		result.CompletedAt = time.Now().UTC()
		return result, nil
	}
	w.logBackfill("aggregate fact backfill started", tenantID, "definitions", len(definitions), "force", force)

	if err := w.markDefinitionsDirty(ctx, tenantID, definitions); err != nil {
		return result, err
	}
	// Dirty markers deliberately remain after any later failure. That makes a
	// partial rebuild unavailable to readers and makes ingestion demand a retry.
	for _, definition := range definitions {
		definitionStartedAt := time.Now().UTC()
		if err := w.deleteDefinitionGeneration(ctx, tenantID, definition); err != nil {
			return result, err
		}
		groups, keys, err := w.rebuildDefinition(ctx, tenantID, definition, startedAt)
		if err != nil {
			return result, fmt.Errorf("backfill definition %s: %w", definition.ID, err)
		}
		definitionCompletedAt := time.Now().UTC()
		definitionResult := ports.AggregateFactDefinitionBackfillResult{
			ID: definition.ID, TableName: definition.TableName, Signature: definition.Signature,
			DimensionFields: append([]string(nil), definition.DimensionFields...),
			MeasureField:    definition.MeasureField, MaxWindowSeconds: definition.MaxWindowSeconds,
			GroupsWritten: groups, BucketKeysWritten: keys,
			StartedAt: definitionStartedAt, CompletedAt: definitionCompletedAt,
			DurationMS: definitionCompletedAt.Sub(definitionStartedAt).Milliseconds(),
		}
		result.DefinitionResults = append(result.DefinitionResults, definitionResult)
		result.GroupsWritten += groups
		result.BucketKeysWritten += keys
		w.logBackfill("aggregate fact definition backfilled", tenantID,
			"definition_id", definition.ID, "table_name", definition.TableName,
			"groups_written", groups, "bucket_keys_written", keys,
			"duration_ms", definitionResult.DurationMS)
	}

	current, err := w.listActiveForTenant(ctx, tenantID.String())
	if err != nil {
		return result, fmt.Errorf("verify aggregate fact registry: %w", err)
	}
	if !sameDefinitionGenerations(definitions, current) {
		return result, fmt.Errorf("aggregate fact registry changed during backfill; retry against the new generation")
	}

	if err := w.publishBackfill(ctx, tenantID, definitions); err != nil {
		return result, err
	}
	if err := w.deleteRequestMarkers(ctx, tenantID); err != nil {
		return result, err
	}
	if err := w.clearDefinitionsDirty(ctx, tenantID, definitions); err != nil {
		return result, err
	}

	status, err := w.Status(ctx, tenantID)
	if err != nil {
		return result, err
	}
	if !status.Ready {
		return result, fmt.Errorf("aggregate fact backfill completed but readiness could not be confirmed")
	}
	result.Rebuilt = true
	result.DefinitionsRebuilt = len(definitions)
	result.CompletedAt = time.Now().UTC()
	result.Status = status
	w.logBackfill("aggregate fact backfill completed", tenantID,
		"definitions", result.DefinitionsRebuilt, "groups_written", result.GroupsWritten,
		"bucket_keys_written", result.BucketKeysWritten, "duration_ms", time.Since(startedAt).Milliseconds())
	return result, nil
}

func (w *Writer) logBackfill(message string, tenantID uuid.UUID, args ...any) {
	if w.logger == nil {
		return
	}
	fields := []any{"tenant_id", tenantID.String()}
	fields = append(fields, args...)
	w.logger.Info(message, fields...)
}

func (w *Writer) markDefinitionsDirty(ctx context.Context, tenantID uuid.UUID, definitions []ports.AggregateFactDefinition) error {
	pipe := w.client.Pipeline()
	for _, definition := range definitions {
		pipe.Set(ctx, factDirtyKey(tenantID.String(), definition), "backfill", 0)
	}
	if len(definitions) == 0 {
		return nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("mark aggregate facts dirty: %w", err)
	}
	return nil
}

func (w *Writer) clearDefinitionsDirty(ctx context.Context, tenantID uuid.UUID, definitions []ports.AggregateFactDefinition) error {
	if len(definitions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		keys = append(keys, factDirtyKey(tenantID.String(), definition))
	}
	if err := w.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("clear aggregate fact dirty markers: %w", err)
	}
	return nil
}

func (w *Writer) deleteDefinitionGeneration(ctx context.Context, tenantID uuid.UUID, definition ports.AggregateFactDefinition) error {
	pattern := fmt.Sprintf("facts:{%s}:definition:%s:v%d:*", tenantID, definition.ID, definition.Version)
	dirtyKey := factDirtyKey(tenantID.String(), definition)
	return w.scanDelete(ctx, pattern, func(key string) bool { return key != dirtyKey })
}

func (w *Writer) deleteRequestMarkers(ctx context.Context, tenantID uuid.UUID) error {
	return w.scanDelete(ctx, fmt.Sprintf("facts:{%s}:request:*", tenantID), nil)
}

func (w *Writer) scanDelete(ctx context.Context, pattern string, include func(string) bool) error {
	var cursor uint64
	for {
		keys, next, err := w.client.Scan(ctx, cursor, pattern, 1_000).Result()
		if err != nil {
			return fmt.Errorf("scan aggregate fact keys: %w", err)
		}
		selected := keys[:0]
		for _, key := range keys {
			if include == nil || include(key) {
				selected = append(selected, key)
			}
		}
		if len(selected) > 0 {
			if err := w.client.Unlink(ctx, selected...).Err(); err != nil {
				return fmt.Errorf("delete aggregate fact keys: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (w *Writer) rebuildDefinition(ctx context.Context, tenantID uuid.UUID, definition ports.AggregateFactDefinition, now time.Time) (int64, int64, error) {
	query := backfillQuery(tenantID, definition)
	rows, err := w.db.Query(ctx, query)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	resolutionByName := make(map[string]resolutionSpec)
	for _, resolution := range resolutions(definition) {
		resolutionByName[resolution.name] = resolution
	}

	pipe := w.client.Pipeline()
	queued := 0
	var groupsWritten int64
	var keysWritten int64
	lastKey := ""
	flush := func() error {
		if queued == 0 {
			return nil
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		pipe = w.client.Pipeline()
		queued = 0
		return nil
	}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return groupsWritten, keysWritten, err
		}
		if len(values) != len(definition.DimensionFields)+4 {
			return groupsWritten, keysWritten, fmt.Errorf("unexpected aggregate fact backfill row width")
		}
		resolutionName, ok := values[0].(string)
		if !ok {
			return groupsWritten, keysWritten, fmt.Errorf("aggregate fact resolution has unexpected type %T", values[0])
		}
		resolution, exists := resolutionByName[resolutionName]
		if !exists {
			return groupsWritten, keysWritten, fmt.Errorf("aggregate fact backfill returned unknown resolution %q", resolutionName)
		}
		bucket, ok := values[1].(time.Time)
		if !ok {
			return groupsWritten, keysWritten, fmt.Errorf("aggregate fact bucket has unexpected type %T", values[1])
		}
		record := make(map[string]any, len(definition.DimensionFields))
		for index, field := range definition.DimensionFields {
			record[field] = values[index+2]
		}
		dimension, ok := dimensionValue(record, definition.DimensionFields)
		if !ok {
			continue
		}
		sum, ok := values[len(values)-2].(float64)
		if !ok {
			return groupsWritten, keysWritten, fmt.Errorf("aggregate fact sum has unexpected type %T", values[len(values)-2])
		}
		count, ok := values[len(values)-1].(int64)
		if !ok {
			return groupsWritten, keysWritten, fmt.Errorf("aggregate fact count has unexpected type %T", values[len(values)-1])
		}
		key := factBucketKey(tenantID.String(), definition, resolutionName, bucket.UTC())
		fields := make(map[string]any, 2)
		if definition.NeedsSum {
			fields["s:"+dimension] = sum
		}
		if definition.NeedsCount {
			fields["c:"+dimension] = count
		}
		if len(fields) == 0 {
			continue
		}
		expiry := retainedFactExpiry(bucket.UTC(), resolution, now)
		pipe.HSet(ctx, key, fields)
		pipe.ExpireAt(ctx, key, expiry)
		queued += 2
		groupsWritten++
		if key != lastKey {
			keysWritten++
			lastKey = key
		}
		if queued >= backfillPipelineSize {
			if err := flush(); err != nil {
				return groupsWritten, keysWritten, err
			}
		}
		if groupsWritten%100_000 == 0 {
			w.logBackfill("aggregate fact backfill progress", tenantID,
				"definition_id", definition.ID, "resolution", resolutionName,
				"groups_written", groupsWritten, "bucket_keys_written", keysWritten)
		}
	}
	if err := rows.Err(); err != nil {
		return groupsWritten, keysWritten, err
	}
	if err := flush(); err != nil {
		return groupsWritten, keysWritten, err
	}
	return groupsWritten, keysWritten, nil
}

func backfillQuery(tenantID uuid.UUID, definition ports.AggregateFactDefinition) string {
	schema := "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
	table := pgx.Identifier{schema, definition.TableName}.Sanitize()
	eventField := pgx.Identifier{definition.EventTimeField}.Sanitize()
	dimensions := make([]string, 0, len(definition.DimensionFields))
	for _, field := range definition.DimensionFields {
		dimensions = append(dimensions, pgx.Identifier{field}.Sanitize())
	}
	measure := pgx.Identifier{definition.MeasureField}.Sanitize()
	sumExpression := "0::double precision"
	if definition.NeedsSum {
		sumExpression = fmt.Sprintf("COALESCE(SUM(%s), 0)::double precision", measure)
	}
	countExpression := "0::bigint"
	if definition.NeedsCount {
		countExpression = fmt.Sprintf("COUNT(%s)::bigint", measure)
	}
	resolutionList := resolutions(definition)
	finest := resolutionList[0]
	baseBucketExpression := fmt.Sprintf(
		"to_timestamp(floor(extract(epoch FROM %s) / %d) * %d)",
		eventField, int64(finest.size/time.Second), int64(finest.size/time.Second),
	)
	baseSelectFields := append([]string{baseBucketExpression + " AS fact_bucket"}, dimensions...)
	baseSelectFields = append(baseSelectFields, sumExpression+" AS fact_sum", countExpression+" AS fact_count")
	where := []string{eventField + " IS NOT NULL"}
	for _, dimension := range dimensions {
		where = append(where, dimension+" IS NOT NULL")
	}
	baseGroupPositions := make([]string, 0, len(dimensions)+1)
	for index := 1; index <= len(dimensions)+1; index++ {
		baseGroupPositions = append(baseGroupPositions, fmt.Sprint(index))
	}

	selects := make([]string, 0, len(resolutionList))
	for _, resolution := range resolutionList {
		if resolution.name == finest.name {
			fields := append([]string{fmt.Sprintf("'%s'::text AS fact_resolution", resolution.name), "fact_bucket"}, dimensions...)
			fields = append(fields, "fact_sum", "fact_count")
			selects = append(selects, "SELECT "+strings.Join(fields, ", ")+" FROM base")
			continue
		}
		bucketExpression := fmt.Sprintf(
			"to_timestamp(floor(extract(epoch FROM fact_bucket) / %d) * %d)",
			int64(resolution.size/time.Second), int64(resolution.size/time.Second),
		)
		fields := append([]string{fmt.Sprintf("'%s'::text AS fact_resolution", resolution.name), bucketExpression + " AS fact_bucket"}, dimensions...)
		fields = append(fields, "SUM(fact_sum)::double precision AS fact_sum", "SUM(fact_count)::bigint AS fact_count")
		groupPositions := make([]string, 0, len(dimensions)+1)
		for index := 2; index <= len(dimensions)+2; index++ {
			groupPositions = append(groupPositions, fmt.Sprint(index))
		}
		selects = append(selects, "SELECT "+strings.Join(fields, ", ")+" FROM base GROUP BY "+strings.Join(groupPositions, ", "))
	}
	return fmt.Sprintf(
		"WITH base AS MATERIALIZED (SELECT %s FROM %s WHERE %s GROUP BY %s) %s ORDER BY 1, 2",
		strings.Join(baseSelectFields, ", "), table, strings.Join(where, " AND "), strings.Join(baseGroupPositions, ", "),
		strings.Join(selects, " UNION ALL "),
	)
}

func (w *Writer) publishBackfill(ctx context.Context, tenantID uuid.UUID, definitions []ports.AggregateFactDefinition) error {
	pipe := w.client.Pipeline()
	for _, definition := range definitions {
		pipe.Set(ctx, factCoverageKey(tenantID.String(), definition), definition.CoverageToken+"|all", 0)
	}
	if len(definitions) > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("publish aggregate fact coverage: %w", err)
		}
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin aggregate fact backfill completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, definition := range definitions {
		command, err := tx.Exec(ctx, `
			UPDATE core.aggregate_fact_definitions
			SET backfill_required = FALSE, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2 AND version = $3 AND coverage_token = $4
		`, definition.ID, tenantID, definition.Version, definition.CoverageToken)
		if err != nil {
			return fmt.Errorf("mark aggregate fact definition backfilled: %w", err)
		}
		if command.RowsAffected() != 1 {
			return fmt.Errorf("aggregate fact definition changed while publishing backfill")
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE core_ingestion.idempotency_keys
		SET fact_marker = NULL, fact_manifest = NULL, fact_status = NULL, fact_updated_at = NULL
		WHERE tenant_id = $1 AND fact_status = 'pending'
	`, tenantID); err != nil {
		return fmt.Errorf("reconcile aggregate fact idempotency receipts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit aggregate fact backfill completion: %w", err)
	}
	return nil
}

func (w *Writer) listActiveForTenant(ctx context.Context, tenantID string) ([]ports.AggregateFactDefinition, error) {
	rows, err := w.db.Query(ctx, `
		SELECT DISTINCT d.id, d.tenant_id, d.table_name, d.signature,
		       d.dimension_fields, d.event_time_field, d.measure_field,
		       d.needs_sum, d.needs_count, d.max_window_seconds,
		       d.minute_enabled, d.backfill_required, d.coverage_token, d.version,
		       COALESCE(v.version, 0)
		FROM core.aggregate_fact_definitions d
		JOIN core.aggregate_fact_bindings b ON b.definition_id = d.id AND b.tenant_id = d.tenant_id
		JOIN core.scenarios s ON s.id = b.scenario_id AND s.live_iteration_id = b.iteration_id
		LEFT JOIN core.aggregate_fact_registry_versions v
		  ON v.tenant_id = d.tenant_id AND v.table_name = d.table_name
		WHERE d.tenant_id = $1
		ORDER BY d.table_name, d.signature
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	definitions := make([]ports.AggregateFactDefinition, 0)
	for rows.Next() {
		var definition ports.AggregateFactDefinition
		if err := rows.Scan(&definition.ID, &definition.TenantID, &definition.TableName, &definition.Signature,
			&definition.DimensionFields, &definition.EventTimeField, &definition.MeasureField,
			&definition.NeedsSum, &definition.NeedsCount, &definition.MaxWindowSeconds,
			&definition.MinuteEnabled, &definition.BackfillRequired, &definition.CoverageToken,
			&definition.Version, &definition.RegistryVersion); err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func sameDefinitionGenerations(left, right []ports.AggregateFactDefinition) bool {
	if len(left) != len(right) {
		return false
	}
	identity := func(definition ports.AggregateFactDefinition) string {
		return fmt.Sprintf("%s|%s|%d|%s|%d", definition.ID, definition.TableName, definition.Version, definition.CoverageToken, definition.RegistryVersion)
	}
	leftIDs := make([]string, len(left))
	rightIDs := make([]string, len(right))
	for index := range left {
		leftIDs[index] = identity(left[index])
	}
	for index := range right {
		rightIDs[index] = identity(right[index])
	}
	sort.Strings(leftIDs)
	sort.Strings(rightIDs)
	return strings.Join(leftIDs, "\n") == strings.Join(rightIDs, "\n")
}

var _ ports.AggregateFactBackfiller = (*Writer)(nil)
