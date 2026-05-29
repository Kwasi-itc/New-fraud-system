package postgres

import (
	"context"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

type ScreeningRepository struct{ q queryable }
type ScreeningMatchRepository struct{ q queryable }
type ScreeningCommentRepository struct{ q queryable }
type ScreeningWhitelistRepository struct{ q queryable }
type ScreeningFileRepository struct{ q queryable }
type ContinuousConfigRepository struct{ q queryable }
type MonitoredObjectRepository struct{ q queryable }
type DatasetUpdateJobRepository struct{ q queryable }

func NewScreeningRepository(q queryable) ScreeningRepository {
	return ScreeningRepository{q: q}
}

func NewScreeningMatchRepository(q queryable) ScreeningMatchRepository {
	return ScreeningMatchRepository{q: q}
}

func NewScreeningCommentRepository(q queryable) ScreeningCommentRepository {
	return ScreeningCommentRepository{q: q}
}

func NewScreeningWhitelistRepository(q queryable) ScreeningWhitelistRepository {
	return ScreeningWhitelistRepository{q: q}
}

func NewScreeningFileRepository(q queryable) ScreeningFileRepository {
	return ScreeningFileRepository{q: q}
}

func NewContinuousConfigRepository(q queryable) ContinuousConfigRepository {
	return ContinuousConfigRepository{q: q}
}

func NewMonitoredObjectRepository(q queryable) MonitoredObjectRepository {
	return MonitoredObjectRepository{q: q}
}

func NewDatasetUpdateJobRepository(q queryable) DatasetUpdateJobRepository {
	return DatasetUpdateJobRepository{q: q}
}

func (r ScreeningRepository) Create(ctx context.Context, item screening.Screening) (screening.Screening, error) {
	const stmt = `
		insert into screening.screenings (
			id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		returning id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
	`
	return scanScreening(r.q.QueryRow(ctx, stmt,
		item.ID, item.TenantID, nullIfEmpty(item.DecisionID), nullIfEmpty(item.ScenarioID), nullIfEmpty(item.ScreeningConfigID), nullIfEmpty(item.IdempotencyKey),
		item.Provider, item.ObjectType, item.ObjectID, string(item.Status), item.RequestJSON, item.ResponseJSON, item.ProviderReference,
		item.LastError, item.IsManual, item.IsArchived, item.Partial, item.UniqueCounterpartyIdentifier,
		item.CreatedAt, item.UpdatedAt, item.SentAt, item.CompletedAt, item.FailedAt,
	))
}

func (r ScreeningRepository) GetByID(ctx context.Context, tenantID, screeningID string) (screening.Screening, error) {
	const stmt = `
		select id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
		from screening.screenings
		where tenant_id = $1 and id = $2
	`
	return scanScreening(r.q.QueryRow(ctx, stmt, tenantID, screeningID))
}

func (r ScreeningRepository) GetByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (screening.Screening, error) {
	const stmt = `
		select id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
		from screening.screenings
		where tenant_id = $1 and idempotency_key = $2
	`
	return scanScreening(r.q.QueryRow(ctx, stmt, tenantID, idempotencyKey))
}

func (r ScreeningRepository) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Screening, error) {
	const stmt = `
		select id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
		from screening.screenings
		where tenant_id = $1 and decision_id = $2
		order by created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []screening.Screening
	for rows.Next() {
		item, err := scanScreening(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningRepository) ListByStatus(ctx context.Context, status screening.Status, limit int) ([]screening.Screening, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
		from screening.screenings
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []screening.Screening
	for rows.Next() {
		item, err := scanScreening(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningRepository) Update(ctx context.Context, item screening.Screening) (screening.Screening, error) {
	const stmt = `
		update screening.screenings
		set decision_id = $1,
			scenario_id = $2,
			screening_config_id = $3,
			idempotency_key = $4,
			provider = $5,
			object_type = $6,
			object_id = $7,
			status = $8,
			request_json = $9,
			response_json = $10,
			provider_reference = $11,
			last_error = $12,
			is_manual = $13,
			is_archived = $14,
			partial = $15,
			unique_counterparty_identifier = $16,
			updated_at = $17,
			sent_at = $18,
			completed_at = $19,
			failed_at = $20
		where tenant_id = $21 and id = $22
		returning id, tenant_id, decision_id, scenario_id, screening_config_id, idempotency_key, provider, object_type, object_id, status,
			request_json, response_json, provider_reference, last_error, is_manual, is_archived, partial,
			unique_counterparty_identifier, created_at, updated_at, sent_at, completed_at, failed_at
	`
	return scanScreening(r.q.QueryRow(ctx, stmt,
		nullIfEmpty(item.DecisionID), nullIfEmpty(item.ScenarioID), nullIfEmpty(item.ScreeningConfigID), nullIfEmpty(item.IdempotencyKey),
		item.Provider, item.ObjectType, item.ObjectID, string(item.Status), item.RequestJSON, item.ResponseJSON,
		item.ProviderReference, item.LastError, item.IsManual, item.IsArchived, item.Partial,
		item.UniqueCounterpartyIdentifier, item.UpdatedAt, item.SentAt, item.CompletedAt, item.FailedAt,
		item.TenantID, item.ID,
	))
}

func (r ScreeningMatchRepository) ReplaceForScreening(ctx context.Context, screeningID string, items []screening.Match) error {
	if _, err := r.q.Exec(ctx, `delete from screening.screening_matches where screening_id = $1`, screeningID); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	const stmt = `
		insert into screening.screening_matches (
			id, tenant_id, screening_id, entity_id, provider, status, name, score, payload, matched_texts,
			unique_counterparty_identifier, enriched, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`
	for _, item := range items {
		if _, err := r.q.Exec(ctx, stmt, item.ID, item.TenantID, item.ScreeningID, item.EntityID, item.Provider,
			string(item.Status), item.Name, item.Score, item.Payload, pq.Array(item.MatchedTexts),
			item.UniqueCounterpartyIdentifier, item.Enriched, item.CreatedAt, item.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (r ScreeningMatchRepository) ListByScreening(ctx context.Context, tenantID, screeningID string) ([]screening.Match, error) {
	const stmt = `
		select id, tenant_id, screening_id, entity_id, provider, status, name, score, payload, matched_texts,
			unique_counterparty_identifier, enriched, created_at, updated_at
		from screening.screening_matches
		where tenant_id = $1 and screening_id = $2
		order by score desc, created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, screeningID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []screening.Match
	for rows.Next() {
		item, err := scanMatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningMatchRepository) GetByID(ctx context.Context, tenantID, matchID string) (screening.Match, error) {
	const stmt = `
		select id, tenant_id, screening_id, entity_id, provider, status, name, score, payload, matched_texts,
			unique_counterparty_identifier, enriched, created_at, updated_at
		from screening.screening_matches
		where tenant_id = $1 and id = $2
	`
	return scanMatch(r.q.QueryRow(ctx, stmt, tenantID, matchID))
}

func (r ScreeningMatchRepository) Update(ctx context.Context, item screening.Match) (screening.Match, error) {
	const stmt = `
		update screening.screening_matches
		set status = $1,
			name = $2,
			score = $3,
			payload = $4,
			matched_texts = $5,
			unique_counterparty_identifier = $6,
			enriched = $7,
			updated_at = $8
		where tenant_id = $9 and id = $10
		returning id, tenant_id, screening_id, entity_id, provider, status, name, score, payload, matched_texts,
			unique_counterparty_identifier, enriched, created_at, updated_at
	`
	return scanMatch(r.q.QueryRow(ctx, stmt, string(item.Status), item.Name, item.Score, item.Payload,
		pq.Array(item.MatchedTexts), item.UniqueCounterpartyIdentifier, item.Enriched, item.UpdatedAt, item.TenantID, item.ID))
}

func (r ScreeningMatchRepository) CountPendingByScreening(ctx context.Context, screeningID string) (int, error) {
	const stmt = `
		select count(*)
		from screening.screening_matches
		where screening_id = $1 and status = 'pending'
	`
	var count int
	err := r.q.QueryRow(ctx, stmt, screeningID).Scan(&count)
	return count, err
}

func (r ScreeningCommentRepository) Create(ctx context.Context, item screening.Comment) (screening.Comment, error) {
	const stmt = `
		insert into screening.screening_match_comments (id, tenant_id, match_id, comment_text, author_id, created_at)
		values ($1,$2,$3,$4,$5,$6)
		returning id, tenant_id, match_id, comment_text, author_id, created_at
	`
	var out screening.Comment
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.MatchID, item.CommentText, item.AuthorID, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.MatchID, &out.CommentText, &out.AuthorID, &out.CreatedAt)
	return out, err
}

func (r ScreeningCommentRepository) ListByMatchIDs(ctx context.Context, tenantID string, matchIDs []string) ([]screening.Comment, error) {
	if len(matchIDs) == 0 {
		return []screening.Comment{}, nil
	}
	const stmt = `
		select id, tenant_id, match_id, comment_text, author_id, created_at
		from screening.screening_match_comments
		where tenant_id = $1 and match_id = any($2)
		order by created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, pq.Array(matchIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []screening.Comment
	for rows.Next() {
		var item screening.Comment
		if err := rows.Scan(&item.ID, &item.TenantID, &item.MatchID, &item.CommentText, &item.AuthorID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningWhitelistRepository) Create(ctx context.Context, item screening.WhitelistEntry) (screening.WhitelistEntry, error) {
	const stmt = `
		insert into screening.screening_whitelist_entries (
			id, tenant_id, entity_id, unique_counterparty_identifier, reviewer_id, created_at
		) values ($1,$2,$3,$4,$5,$6)
		returning id, tenant_id, entity_id, unique_counterparty_identifier, reviewer_id, created_at
	`
	var out screening.WhitelistEntry
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.EntityID, item.UniqueCounterpartyIdentifier, item.ReviewerID, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.EntityID, &out.UniqueCounterpartyIdentifier, &out.ReviewerID, &out.CreatedAt)
	return out, err
}

func (r ScreeningWhitelistRepository) Delete(ctx context.Context, tenantID, entityID string, counterpartyIdentifier *string) error {
	stmt := `
		delete from screening.screening_whitelist_entries
		where tenant_id = $1 and entity_id = $2
	`
	args := []any{tenantID, entityID}
	if counterpartyIdentifier != nil {
		stmt += ` and unique_counterparty_identifier = $3`
		args = append(args, *counterpartyIdentifier)
	}
	_, err := r.q.Exec(ctx, stmt, args...)
	return err
}

func (r ScreeningWhitelistRepository) Search(ctx context.Context, tenantID string, entityID, counterpartyIdentifier *string) ([]screening.WhitelistEntry, error) {
	var builder strings.Builder
	builder.WriteString(`
		select id, tenant_id, entity_id, unique_counterparty_identifier, reviewer_id, created_at
		from screening.screening_whitelist_entries
		where tenant_id = $1
	`)

	args := []any{tenantID}
	if entityID != nil {
		builder.WriteString(` and entity_id = $2`)
		args = append(args, *entityID)
	}
	if counterpartyIdentifier != nil {
		position := len(args) + 1
		builder.WriteString(` and unique_counterparty_identifier = $` + itoa(position))
		args = append(args, *counterpartyIdentifier)
	}
	builder.WriteString(` order by created_at desc`)

	rows, err := r.q.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []screening.WhitelistEntry
	for rows.Next() {
		var item screening.WhitelistEntry
		if err := rows.Scan(&item.ID, &item.TenantID, &item.EntityID, &item.UniqueCounterpartyIdentifier, &item.ReviewerID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningFileRepository) Create(ctx context.Context, item screening.File) (screening.File, error) {
	const stmt = `
		insert into screening.screening_files (
			id, tenant_id, screening_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		returning id, tenant_id, screening_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at
	`
	var out screening.File
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScreeningID, item.FileName, item.ContentType, item.FileSize, item.StorageKey, item.UploadedBy, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScreeningID, &out.FileName, &out.ContentType, &out.FileSize, &out.StorageKey, &out.UploadedBy, &out.CreatedAt)
	return out, err
}

func (r ScreeningFileRepository) GetByID(ctx context.Context, tenantID, screeningID, fileID string) (screening.File, error) {
	const stmt = `
		select id, tenant_id, screening_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at
		from screening.screening_files
		where tenant_id = $1 and screening_id = $2 and id = $3
	`
	var out screening.File
	err := r.q.QueryRow(ctx, stmt, tenantID, screeningID, fileID).
		Scan(&out.ID, &out.TenantID, &out.ScreeningID, &out.FileName, &out.ContentType, &out.FileSize, &out.StorageKey, &out.UploadedBy, &out.CreatedAt)
	return out, err
}

func (r ScreeningFileRepository) ListByScreening(ctx context.Context, tenantID, screeningID string) ([]screening.File, error) {
	const stmt = `
		select id, tenant_id, screening_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at
		from screening.screening_files
		where tenant_id = $1 and screening_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, screeningID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.File
	for rows.Next() {
		var item screening.File
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScreeningID, &item.FileName, &item.ContentType, &item.FileSize, &item.StorageKey, &item.UploadedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ContinuousConfigRepository) Create(ctx context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error) {
	const stmt = `
		insert into screening.continuous_screening_configs (
			id, tenant_id, name, object_type, provider, field_map_json, review_inbox_id, enabled, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		returning id, tenant_id, name, object_type, provider, field_map_json, review_inbox_id, enabled, created_at, updated_at
	`
	return scanContinuousConfig(r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.Name, item.ObjectType, item.Provider, item.FieldMapJSON, item.ReviewInboxID, item.Enabled, item.CreatedAt, item.UpdatedAt))
}

func (r ContinuousConfigRepository) GetByID(ctx context.Context, tenantID, configID string) (screening.ContinuousConfig, error) {
	const stmt = `
		select id, tenant_id, name, object_type, provider, field_map_json, review_inbox_id, enabled, created_at, updated_at
		from screening.continuous_screening_configs
		where tenant_id = $1 and id = $2
	`
	return scanContinuousConfig(r.q.QueryRow(ctx, stmt, tenantID, configID))
}

func (r ContinuousConfigRepository) ListByTenant(ctx context.Context, tenantID string) ([]screening.ContinuousConfig, error) {
	const stmt = `
		select id, tenant_id, name, object_type, provider, field_map_json, review_inbox_id, enabled, created_at, updated_at
		from screening.continuous_screening_configs
		where tenant_id = $1
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.ContinuousConfig
	for rows.Next() {
		item, err := scanContinuousConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ContinuousConfigRepository) Update(ctx context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error) {
	const stmt = `
		update screening.continuous_screening_configs
		set name = $1, object_type = $2, provider = $3, field_map_json = $4, review_inbox_id = $5, enabled = $6, updated_at = $7
		where tenant_id = $8 and id = $9
		returning id, tenant_id, name, object_type, provider, field_map_json, review_inbox_id, enabled, created_at, updated_at
	`
	return scanContinuousConfig(r.q.QueryRow(ctx, stmt, item.Name, item.ObjectType, item.Provider, item.FieldMapJSON, item.ReviewInboxID, item.Enabled, item.UpdatedAt, item.TenantID, item.ID))
}

func (r ContinuousConfigRepository) Delete(ctx context.Context, tenantID, configID string) error {
	_, err := r.q.Exec(ctx, `delete from screening.continuous_screening_configs where tenant_id = $1 and id = $2`, tenantID, configID)
	return err
}

func (r MonitoredObjectRepository) Create(ctx context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error) {
	const stmt = `
		insert into screening.monitored_objects (
			id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		returning id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
	`
	return scanMonitoredObject(r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ConfigID, item.ObjectType, item.ObjectID, string(item.Status), item.AttributesJSON, item.CreatedAt, item.UpdatedAt, item.LastScreenedAt))
}

func (r MonitoredObjectRepository) GetByID(ctx context.Context, tenantID, monitoredObjectID string) (screening.MonitoredObject, error) {
	const stmt = `
		select id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
		from screening.monitored_objects
		where tenant_id = $1 and id = $2
	`
	return scanMonitoredObject(r.q.QueryRow(ctx, stmt, tenantID, monitoredObjectID))
}

func (r MonitoredObjectRepository) ListByConfig(ctx context.Context, tenantID, configID string) ([]screening.MonitoredObject, error) {
	const stmt = `
		select id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
		from screening.monitored_objects
		where tenant_id = $1 and config_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, configID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.MonitoredObject
	for rows.Next() {
		item, err := scanMonitoredObject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r MonitoredObjectRepository) ListByStatus(ctx context.Context, status screening.MonitoredObjectStatus, limit int) ([]screening.MonitoredObject, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
		from screening.monitored_objects
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.MonitoredObject
	for rows.Next() {
		item, err := scanMonitoredObject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r MonitoredObjectRepository) ListByTenantAndStatus(ctx context.Context, tenantID string, status screening.MonitoredObjectStatus, limit int) ([]screening.MonitoredObject, error) {
	if limit <= 0 {
		limit = 500
	}
	const stmt = `
		select id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
		from screening.monitored_objects
		where tenant_id = $1 and status = $2
		order by created_at asc
		limit $3
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.MonitoredObject
	for rows.Next() {
		item, err := scanMonitoredObject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r MonitoredObjectRepository) Update(ctx context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error) {
	const stmt = `
		update screening.monitored_objects
		set status = $1, attributes_json = $2, updated_at = $3, last_screened_at = $4
		where tenant_id = $5 and id = $6
		returning id, tenant_id, config_id, object_type, object_id, status, attributes_json, created_at, updated_at, last_screened_at
	`
	return scanMonitoredObject(r.q.QueryRow(ctx, stmt, string(item.Status), item.AttributesJSON, item.UpdatedAt, item.LastScreenedAt, item.TenantID, item.ID))
}

func (r MonitoredObjectRepository) Delete(ctx context.Context, tenantID, monitoredObjectID string) error {
	_, err := r.q.Exec(ctx, `delete from screening.monitored_objects where tenant_id = $1 and id = $2`, tenantID, monitoredObjectID)
	return err
}

func (r DatasetUpdateJobRepository) Create(ctx context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error) {
	const stmt = `
		insert into screening.dataset_update_jobs (
			id, tenant_id, provider, job_type, status, cursor, result_json, last_error, attempt_count, created_at, updated_at, started_at, completed_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		returning id, tenant_id, provider, job_type, status, cursor, result_json, last_error, attempt_count, created_at, updated_at, started_at, completed_at
	`
	return scanDatasetUpdateJob(r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.Provider, item.JobType, string(item.Status), item.Cursor, item.ResultJSON, item.LastError, item.AttemptCount, item.CreatedAt, item.UpdatedAt, item.StartedAt, item.CompletedAt))
}

func (r DatasetUpdateJobRepository) GetByID(ctx context.Context, tenantID, jobID string) (screening.DatasetUpdateJob, error) {
	const stmt = `
		select id, tenant_id, provider, job_type, status, cursor, result_json, last_error, attempt_count, created_at, updated_at, started_at, completed_at
		from screening.dataset_update_jobs
		where tenant_id = $1 and id = $2
	`
	return scanDatasetUpdateJob(r.q.QueryRow(ctx, stmt, tenantID, jobID))
}

func (r DatasetUpdateJobRepository) ListByTenant(ctx context.Context, tenantID string) ([]screening.DatasetUpdateJob, error) {
	const stmt = `
		select id, tenant_id, provider, job_type, status, cursor, result_json, last_error, attempt_count, created_at, updated_at, started_at, completed_at
		from screening.dataset_update_jobs
		where tenant_id = $1
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.DatasetUpdateJob
	for rows.Next() {
		item, err := scanDatasetUpdateJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r DatasetUpdateJobRepository) ListByStatus(ctx context.Context, status screening.DatasetUpdateJobStatus, limit int) ([]screening.DatasetUpdateJob, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, provider, job_type, status, cursor, result_json, last_error, attempt_count, created_at, updated_at, started_at, completed_at
		from screening.dataset_update_jobs
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.DatasetUpdateJob
	for rows.Next() {
		item, err := scanDatasetUpdateJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r DatasetUpdateJobRepository) Update(ctx context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error) {
	const stmt = `
		update screening.dataset_update_jobs
		set provider = $1,
			job_type = $2,
			status = $3,
			cursor = $4,
			result_json = $5,
			last_error = $6,
			attempt_count = $7,
			updated_at = $8,
			started_at = $9,
			completed_at = $10
		where tenant_id = $11 and id = $12
		returning id, tenant_id, provider, job_type, status, cursor, result_json, last_error, attempt_count, created_at, updated_at, started_at, completed_at
	`
	return scanDatasetUpdateJob(r.q.QueryRow(ctx, stmt, item.Provider, item.JobType, string(item.Status), item.Cursor, item.ResultJSON, item.LastError, item.AttemptCount, item.UpdatedAt, item.StartedAt, item.CompletedAt, item.TenantID, item.ID))
}

type scanner interface {
	Scan(dest ...any) error
}

func scanScreening(row scanner) (screening.Screening, error) {
	var item screening.Screening
	var decisionID, scenarioID, configID, idempotencyKey *string
	var status string
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&decisionID,
		&scenarioID,
		&configID,
		&idempotencyKey,
		&item.Provider,
		&item.ObjectType,
		&item.ObjectID,
		&status,
		&item.RequestJSON,
		&item.ResponseJSON,
		&item.ProviderReference,
		&item.LastError,
		&item.IsManual,
		&item.IsArchived,
		&item.Partial,
		&item.UniqueCounterpartyIdentifier,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.SentAt,
		&item.CompletedAt,
		&item.FailedAt,
	)
	if err != nil {
		return screening.Screening{}, err
	}
	item.Status = screening.Status(status)
	if decisionID != nil {
		item.DecisionID = *decisionID
	}
	if scenarioID != nil {
		item.ScenarioID = *scenarioID
	}
	if configID != nil {
		item.ScreeningConfigID = *configID
	}
	if idempotencyKey != nil {
		item.IdempotencyKey = *idempotencyKey
	}
	return item, nil
}

func scanMatch(row scanner) (screening.Match, error) {
	var item screening.Match
	var status string
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.ScreeningID,
		&item.EntityID,
		&item.Provider,
		&status,
		&item.Name,
		&item.Score,
		&item.Payload,
		pq.Array(&item.MatchedTexts),
		&item.UniqueCounterpartyIdentifier,
		&item.Enriched,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return screening.Match{}, err
	}
	item.Status = screening.MatchStatus(status)
	return item, nil
}

func scanContinuousConfig(row scanner) (screening.ContinuousConfig, error) {
	var item screening.ContinuousConfig
	err := row.Scan(&item.ID, &item.TenantID, &item.Name, &item.ObjectType, &item.Provider, &item.FieldMapJSON, &item.ReviewInboxID, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanMonitoredObject(row scanner) (screening.MonitoredObject, error) {
	var item screening.MonitoredObject
	var status string
	err := row.Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.ObjectType, &item.ObjectID, &status, &item.AttributesJSON, &item.CreatedAt, &item.UpdatedAt, &item.LastScreenedAt)
	item.Status = screening.MonitoredObjectStatus(status)
	return item, err
}

func scanDatasetUpdateJob(row scanner) (screening.DatasetUpdateJob, error) {
	var item screening.DatasetUpdateJob
	var status string
	err := row.Scan(&item.ID, &item.TenantID, &item.Provider, &item.JobType, &status, &item.Cursor, &item.ResultJSON, &item.LastError, &item.AttemptCount, &item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt)
	item.Status = screening.DatasetUpdateJobStatus(status)
	return item, err
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
