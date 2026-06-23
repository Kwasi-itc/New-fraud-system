package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
)

type InboxRepository struct{ db queryable }

func NewInboxRepository(db queryable) InboxRepository { return InboxRepository{db: db} }

func (r InboxRepository) Create(ctx context.Context, inbox casepkg.Inbox) (casepkg.Inbox, error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO case_manager.inboxes (
			id, tenant_id, name, status, escalation_inbox_id, auto_assign_enabled,
			case_review_manual, case_review_on_case_created, case_review_on_escalate, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, tenant_id, name, status, escalation_inbox_id, auto_assign_enabled,
			case_review_manual, case_review_on_case_created, case_review_on_escalate, created_at, updated_at`,
		inbox.ID, inbox.TenantID, inbox.Name, inbox.Status, inbox.EscalationInboxID, inbox.AutoAssignEnabled,
		inbox.CaseReviewManual, inbox.CaseReviewOnCaseCreated, inbox.CaseReviewOnEscalate, inbox.CreatedAt, inbox.UpdatedAt)
	return scanInbox(row)
}

func (r InboxRepository) Get(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error) {
	return scanInbox(r.db.QueryRow(ctx, `
		SELECT id, tenant_id, name, status, escalation_inbox_id, auto_assign_enabled,
			case_review_manual, case_review_on_case_created, case_review_on_escalate, created_at, updated_at
		FROM case_manager.inboxes
		WHERE tenant_id=$1 AND id=$2`, tenantID, inboxID))
}

func (r InboxRepository) List(ctx context.Context, tenantID uuid.UUID) ([]casepkg.Inbox, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, name, status, escalation_inbox_id, auto_assign_enabled,
			case_review_manual, case_review_on_case_created, case_review_on_escalate, created_at, updated_at
		FROM case_manager.inboxes
		WHERE tenant_id=$1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanInbox)
}

func (r InboxRepository) Update(ctx context.Context, inbox casepkg.Inbox) (casepkg.Inbox, error) {
	return scanInbox(r.db.QueryRow(ctx, `
		UPDATE case_manager.inboxes
		SET name=$3, status=$4, escalation_inbox_id=$5, auto_assign_enabled=$6,
			case_review_manual=$7, case_review_on_case_created=$8, case_review_on_escalate=$9, updated_at=$10
		WHERE tenant_id=$1 AND id=$2
		RETURNING id, tenant_id, name, status, escalation_inbox_id, auto_assign_enabled,
			case_review_manual, case_review_on_case_created, case_review_on_escalate, created_at, updated_at`,
		inbox.TenantID, inbox.ID, inbox.Name, inbox.Status, inbox.EscalationInboxID, inbox.AutoAssignEnabled,
		inbox.CaseReviewManual, inbox.CaseReviewOnCaseCreated, inbox.CaseReviewOnEscalate, inbox.UpdatedAt))
}

type CaseRepository struct{ db queryable }

func NewCaseRepository(db queryable) CaseRepository { return CaseRepository{db: db} }

func (r CaseRepository) Create(ctx context.Context, item casepkg.Case) (casepkg.Case, error) {
	return scanCase(r.db.QueryRow(ctx, `
		INSERT INTO case_manager.cases (
			id, tenant_id, inbox_id, name, status, outcome, type, assigned_to,
			snoozed_until, boost_reason, review_level, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, tenant_id, inbox_id, name, status, outcome, type, assigned_to,
			snoozed_until, boost_reason, review_level, created_at, updated_at`,
		item.ID, item.TenantID, item.InboxID, item.Name, item.Status, item.Outcome, item.Type, item.AssignedTo,
		item.SnoozedUntil, item.BoostReason, item.ReviewLevel, item.CreatedAt, item.UpdatedAt))
}

func (r CaseRepository) Get(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error) {
	return scanCase(r.db.QueryRow(ctx, `
		SELECT id, tenant_id, inbox_id, name, status, outcome, type, assigned_to,
			snoozed_until, boost_reason, review_level, created_at, updated_at
		FROM case_manager.cases
		WHERE tenant_id=$1 AND id=$2`, tenantID, caseID))
}

func (r CaseRepository) List(ctx context.Context, tenantID uuid.UUID, filters casepkg.CaseFilters, limit int) ([]casepkg.Case, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{tenantID}
	conditions := []string{"tenant_id=$1"}
	if len(filters.Statuses) > 0 {
		statuses := make([]string, 0, len(filters.Statuses))
		for _, status := range filters.Statuses {
			statuses = append(statuses, string(status))
		}
		args = append(args, statuses)
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", len(args)))
	}
	if len(filters.InboxIDs) > 0 {
		args = append(args, filters.InboxIDs)
		conditions = append(conditions, fmt.Sprintf("inbox_id = ANY($%d)", len(args)))
	}
	if filters.Name != "" {
		args = append(args, "%"+strings.ToLower(filters.Name)+"%")
		conditions = append(conditions, fmt.Sprintf("lower(name) LIKE $%d", len(args)))
	}
	if !filters.IncludeSnoozed {
		conditions = append(conditions, "(snoozed_until IS NULL OR snoozed_until <= now())")
	}
	if filters.AssigneeID != "" {
		args = append(args, filters.AssigneeID)
		conditions = append(conditions, fmt.Sprintf("assigned_to=$%d", len(args)))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT id, tenant_id, inbox_id, name, status, outcome, type, assigned_to,
			snoozed_until, boost_reason, review_level, created_at, updated_at
		FROM case_manager.cases
		WHERE %s
		ORDER BY boost_reason IS NULL ASC, created_at DESC, id DESC
		LIMIT $%d`, strings.Join(conditions, " AND "), len(args))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanCase)
}

func (r CaseRepository) Update(ctx context.Context, item casepkg.Case) (casepkg.Case, error) {
	return scanCase(r.db.QueryRow(ctx, `
		UPDATE case_manager.cases
		SET inbox_id=$3, name=$4, status=$5, outcome=$6, type=$7, assigned_to=$8,
			snoozed_until=$9, boost_reason=$10, review_level=$11, updated_at=$12
		WHERE tenant_id=$1 AND id=$2
		RETURNING id, tenant_id, inbox_id, name, status, outcome, type, assigned_to,
			snoozed_until, boost_reason, review_level, created_at, updated_at`,
		item.TenantID, item.ID, item.InboxID, item.Name, item.Status, item.Outcome, item.Type, item.AssignedTo,
		item.SnoozedUntil, item.BoostReason, item.ReviewLevel, item.UpdatedAt))
}

func (r CaseRepository) Assign(ctx context.Context, tenantID, caseID uuid.UUID, assignee *string, updatedAt time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE case_manager.cases SET assigned_to=$3, updated_at=$4 WHERE tenant_id=$1 AND id=$2`,
		tenantID, caseID, assignee, updatedAt)
	return err
}

func (r CaseRepository) Snooze(ctx context.Context, tenantID, caseID uuid.UUID, until *time.Time, updatedAt time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=$3, updated_at=$4 WHERE tenant_id=$1 AND id=$2`,
		tenantID, caseID, until, updatedAt)
	return err
}

type DecisionLinkRepository struct{ db queryable }

func NewDecisionLinkRepository(db queryable) DecisionLinkRepository {
	return DecisionLinkRepository{db: db}
}

func (r DecisionLinkRepository) Create(ctx context.Context, item casepkg.DecisionLink) (casepkg.DecisionLink, error) {
	return scanDecisionLink(r.db.QueryRow(ctx, `
		INSERT INTO case_manager.case_decisions (
			id, tenant_id, case_id, decision_id, scenario_id, object_type, object_id, pivot_value, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, case_id, decision_id) DO UPDATE SET pivot_value=EXCLUDED.pivot_value
		RETURNING id, tenant_id, case_id, decision_id, scenario_id, object_type, object_id, pivot_value, created_at`,
		item.ID, item.TenantID, item.CaseID, item.DecisionID, item.ScenarioID, item.ObjectType, item.ObjectID, item.PivotValue, item.CreatedAt))
}

func (r DecisionLinkRepository) ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.DecisionLink, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, case_id, decision_id, scenario_id, object_type, object_id, pivot_value, created_at
		FROM case_manager.case_decisions
		WHERE tenant_id=$1 AND case_id=$2
		ORDER BY created_at DESC`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanDecisionLink)
}

func (r DecisionLinkRepository) FindOpenByPivot(ctx context.Context, tenantID uuid.UUID, inboxID *uuid.UUID, pivotValue string) (*casepkg.Case, error) {
	args := []any{tenantID, pivotValue}
	inboxCondition := ""
	if inboxID != nil {
		args = append(args, *inboxID)
		inboxCondition = " AND c.inbox_id=$3"
	}
	row := r.db.QueryRow(ctx, `
		SELECT c.id, c.tenant_id, c.inbox_id, c.name, c.status, c.outcome, c.type, c.assigned_to,
			c.snoozed_until, c.boost_reason, c.review_level, c.created_at, c.updated_at
		FROM case_manager.cases c
		JOIN case_manager.case_decisions d ON d.case_id=c.id AND d.tenant_id=c.tenant_id
		WHERE c.tenant_id=$1 AND d.pivot_value=$2 AND c.status <> 'closed'`+inboxCondition+`
		ORDER BY c.created_at DESC
		LIMIT 1`, args...)
	item, err := scanCase(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type ScreeningLinkRepository struct{ db queryable }

func NewScreeningLinkRepository(db queryable) ScreeningLinkRepository {
	return ScreeningLinkRepository{db: db}
}

func (r ScreeningLinkRepository) Create(ctx context.Context, item casepkg.ScreeningLink) (casepkg.ScreeningLink, error) {
	return scanScreeningLink(r.db.QueryRow(ctx, `
		INSERT INTO case_manager.case_screenings (id, tenant_id, case_id, screening_id, match_id, status, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (tenant_id, case_id, screening_id) DO UPDATE SET status=EXCLUDED.status
		RETURNING id, tenant_id, case_id, screening_id, match_id, status, created_at`,
		item.ID, item.TenantID, item.CaseID, item.ScreeningID, item.MatchID, item.Status, item.CreatedAt))
}

func (r ScreeningLinkRepository) ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.ScreeningLink, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, case_id, screening_id, match_id, status, created_at
		FROM case_manager.case_screenings
		WHERE tenant_id=$1 AND case_id=$2
		ORDER BY created_at DESC`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanScreeningLink)
}

type TagRepository struct{ db queryable }

func NewTagRepository(db queryable) TagRepository { return TagRepository{db: db} }

func (r TagRepository) Create(ctx context.Context, tag casepkg.Tag) (casepkg.Tag, error) {
	return scanTag(r.db.QueryRow(ctx, `
		INSERT INTO case_manager.tags (id, tenant_id, target, name, color, deleted_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, tenant_id, target, name, color, deleted_at, created_at, updated_at`,
		tag.ID, tag.TenantID, tag.Target, tag.Name, tag.Color, tag.DeletedAt, tag.CreatedAt, tag.UpdatedAt))
}

func (r TagRepository) Get(ctx context.Context, tenantID, tagID uuid.UUID) (casepkg.Tag, error) {
	return scanTag(r.db.QueryRow(ctx, `
		SELECT id, tenant_id, target, name, color, deleted_at, created_at, updated_at
		FROM case_manager.tags
		WHERE tenant_id=$1 AND id=$2`, tenantID, tagID))
}

func (r TagRepository) List(ctx context.Context, tenantID uuid.UUID, target string) ([]casepkg.Tag, error) {
	args := []any{tenantID}
	condition := "tenant_id=$1 AND deleted_at IS NULL"
	if target != "" {
		args = append(args, target)
		condition += fmt.Sprintf(" AND target=$%d", len(args))
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, target, name, color, deleted_at, created_at, updated_at
		FROM case_manager.tags
		WHERE `+condition+`
		ORDER BY name ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanTag)
}

func (r TagRepository) Update(ctx context.Context, tag casepkg.Tag) (casepkg.Tag, error) {
	return scanTag(r.db.QueryRow(ctx, `
		UPDATE case_manager.tags
		SET name=$3, color=$4, target=$5, updated_at=$6
		WHERE tenant_id=$1 AND id=$2
		RETURNING id, tenant_id, target, name, color, deleted_at, created_at, updated_at`,
		tag.TenantID, tag.ID, tag.Name, tag.Color, tag.Target, tag.UpdatedAt))
}

func (r TagRepository) SoftDelete(ctx context.Context, tenantID, tagID uuid.UUID, deletedAt time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE case_manager.tags SET deleted_at=$3, updated_at=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, tagID, deletedAt)
	return err
}

func (r TagRepository) AddToCase(ctx context.Context, tenantID, caseID, tagID, id uuid.UUID, createdAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO case_manager.case_tags (id, tenant_id, case_id, tag_id, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (tenant_id, case_id, tag_id) WHERE deleted_at IS NULL DO NOTHING`, id, tenantID, caseID, tagID, createdAt)
	return err
}

func (r TagRepository) RemoveFromCase(ctx context.Context, tenantID, caseID, tagID uuid.UUID, deletedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE case_manager.case_tags
		SET deleted_at=$4
		WHERE tenant_id=$1 AND case_id=$2 AND tag_id=$3 AND deleted_at IS NULL`, tenantID, caseID, tagID, deletedAt)
	return err
}

func (r TagRepository) ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.tenant_id, t.target, t.name, t.color, t.deleted_at, t.created_at, t.updated_at
		FROM case_manager.tags t
		JOIN case_manager.case_tags ct ON ct.tag_id=t.id AND ct.tenant_id=t.tenant_id
		WHERE ct.tenant_id=$1 AND ct.case_id=$2 AND ct.deleted_at IS NULL AND t.deleted_at IS NULL
		ORDER BY t.name ASC`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanTag)
}

type EventRepository struct{ db queryable }

func NewEventRepository(db queryable) EventRepository { return EventRepository{db: db} }

func (r EventRepository) Create(ctx context.Context, event casepkg.Event) (casepkg.Event, error) {
	return scanEvent(r.db.QueryRow(ctx, `
		INSERT INTO case_manager.case_events (
			id, tenant_id, case_id, user_id, event_type, additional_note, resource_id, resource_type, new_value, previous_value, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, tenant_id, case_id, user_id, event_type, additional_note, resource_id, resource_type, new_value, previous_value, created_at`,
		event.ID, event.TenantID, event.CaseID, event.UserID, event.EventType, event.AdditionalNote, event.ResourceID, event.ResourceType, event.NewValue, event.PreviousValue, event.CreatedAt))
}

func (r EventRepository) ListByCase(ctx context.Context, tenantID, caseID uuid.UUID, limit int) ([]casepkg.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, case_id, user_id, event_type, additional_note, resource_id, resource_type, new_value, previous_value, created_at
		FROM case_manager.case_events
		WHERE tenant_id=$1 AND case_id=$2
		ORDER BY created_at DESC, id DESC
		LIMIT $3`, tenantID, caseID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanEvent)
}

type FileRepository struct{ db queryable }

func NewFileRepository(db queryable) FileRepository { return FileRepository{db: db} }

func (r FileRepository) Create(ctx context.Context, file casepkg.File) (casepkg.File, error) {
	return scanFile(r.db.QueryRow(ctx, `
		INSERT INTO case_manager.case_files (id, tenant_id, case_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, tenant_id, case_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at`,
		file.ID, file.TenantID, file.CaseID, file.FileName, file.ContentType, file.FileSize, file.StorageKey, file.UploadedBy, file.CreatedAt))
}

func (r FileRepository) ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.File, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, case_id, file_name, content_type, file_size, storage_key, uploaded_by, created_at
		FROM case_manager.case_files
		WHERE tenant_id=$1 AND case_id=$2
		ORDER BY created_at DESC`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRows(rows, scanFile)
}

func scanInbox(row pgx.Row) (casepkg.Inbox, error) {
	var item casepkg.Inbox
	err := row.Scan(&item.ID, &item.TenantID, &item.Name, &item.Status, &item.EscalationInboxID, &item.AutoAssignEnabled,
		&item.CaseReviewManual, &item.CaseReviewOnCaseCreated, &item.CaseReviewOnEscalate, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanCase(row pgx.Row) (casepkg.Case, error) {
	var item casepkg.Case
	err := row.Scan(&item.ID, &item.TenantID, &item.InboxID, &item.Name, &item.Status, &item.Outcome, &item.Type, &item.AssignedTo,
		&item.SnoozedUntil, &item.BoostReason, &item.ReviewLevel, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanDecisionLink(row pgx.Row) (casepkg.DecisionLink, error) {
	var item casepkg.DecisionLink
	err := row.Scan(&item.ID, &item.TenantID, &item.CaseID, &item.DecisionID, &item.ScenarioID, &item.ObjectType, &item.ObjectID, &item.PivotValue, &item.CreatedAt)
	return item, err
}

func scanScreeningLink(row pgx.Row) (casepkg.ScreeningLink, error) {
	var item casepkg.ScreeningLink
	err := row.Scan(&item.ID, &item.TenantID, &item.CaseID, &item.ScreeningID, &item.MatchID, &item.Status, &item.CreatedAt)
	return item, err
}

func scanTag(row pgx.Row) (casepkg.Tag, error) {
	var item casepkg.Tag
	err := row.Scan(&item.ID, &item.TenantID, &item.Target, &item.Name, &item.Color, &item.DeletedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanEvent(row pgx.Row) (casepkg.Event, error) {
	var item casepkg.Event
	err := row.Scan(&item.ID, &item.TenantID, &item.CaseID, &item.UserID, &item.EventType, &item.AdditionalNote, &item.ResourceID, &item.ResourceType, &item.NewValue, &item.PreviousValue, &item.CreatedAt)
	return item, err
}

func scanFile(row pgx.Row) (casepkg.File, error) {
	var item casepkg.File
	err := row.Scan(&item.ID, &item.TenantID, &item.CaseID, &item.FileName, &item.ContentType, &item.FileSize, &item.StorageKey, &item.UploadedBy, &item.CreatedAt)
	return item, err
}

func collectRows[T any](rows pgx.Rows, scan func(pgx.Row) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
