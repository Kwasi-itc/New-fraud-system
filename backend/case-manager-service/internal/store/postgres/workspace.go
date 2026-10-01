package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"strings"
)

type WorkspaceRepository struct{ db queryable }

func (r WorkspaceRepository) HasLink(ctx context.Context, tenant, id uuid.UUID, kind string, resource uuid.UUID) (bool, error) {
	table, column := "case_decisions", "decision_id"
	if kind == "screenings" {
		table, column = "case_screenings", "screening_id"
	} else if kind != "decisions" {
		return false, casepkg.Invalid("invalid evidence kind")
	}
	var found bool
	err := r.db.QueryRow(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM case_manager.%s WHERE tenant_id=$1 AND case_id=$2 AND %s=$3)", table, column), tenant, id, resource).Scan(&found)
	return found, err
}

func workspaceWhere(tenant uuid.UUID, f casepkg.WorkspaceFilters) (string, []any) {
	args := []any{tenant}
	terms := []string{"c.tenant_id=$1"}
	add := func(sql string, arg any) {
		args = append(args, arg)
		terms = append(terms, fmt.Sprintf(sql, len(args)))
	}
	if f.AccessUserID != "" {
		add("EXISTS (SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=c.tenant_id AND u.inbox_id=c.inbox_id AND u.user_id=$%d)", f.AccessUserID)
	}
	if len(f.Statuses) > 0 {
		values := []string{}
		for _, v := range f.Statuses {
			values = append(values, string(v))
		}
		add("c.status=ANY($%d)", values)
	}
	if len(f.InboxIDs) > 0 {
		add("c.inbox_id=ANY($%d)", f.InboxIDs)
	}
	if f.Name != "" {
		add("c.name ILIKE $%d", "%"+strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Name)+"%")
	}
	if !f.IncludeSnoozed {
		terms = append(terms, "(c.snoozed_until IS NULL OR c.snoozed_until<=now())")
	}
	if f.AssigneeID != "" {
		add("c.assigned_to=$%d", f.AssigneeID)
	}
	if f.Overdue {
		terms = append(terms, "c.status<>'closed' AND EXISTS(SELECT 1 FROM case_manager.inboxes i WHERE i.tenant_id=c.tenant_id AND i.id=c.inbox_id AND c.created_at+i.sla_days*interval '24 hours'<=now())")
	}
	if f.Unassigned {
		terms = append(terms, "c.assigned_to IS NULL")
	}
	if f.CreatedFrom != nil {
		add("c.created_at >= $%d", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		add("c.created_at < $%d", *f.CreatedTo)
	}
	if f.ReviewLevel != "" {
		add("c.review_level=$%d", f.ReviewLevel)
	}
	if f.TagID != nil {
		add("EXISTS (SELECT 1 FROM case_manager.case_tags t WHERE t.tenant_id=c.tenant_id AND t.case_id=c.id AND t.tag_id=$%d AND t.deleted_at IS NULL)", *f.TagID)
	}
	if f.RelatedTo != nil {
		args = append(args, *f.RelatedTo)
		n := len(args)
		terms = append(terms, fmt.Sprintf(`c.id<>$%[1]d AND EXISTS (SELECT 1 FROM case_manager.case_decisions d JOIN case_manager.case_decisions source ON source.tenant_id=d.tenant_id AND source.case_id=$%[1]d AND ((source.pivot_value IS NOT NULL AND source.pivot_value=d.pivot_value) OR (source.object_type=d.object_type AND source.object_id=d.object_id AND source.object_id<>'')) WHERE d.tenant_id=c.tenant_id AND d.case_id=c.id)`, n))
	}
	return strings.Join(terms, " AND "), args
}

func (r WorkspaceRepository) Queue(ctx context.Context, tenant uuid.UUID, f casepkg.WorkspaceFilters, limit int) (casepkg.QueuePage, error) {
	result := casepkg.QueuePage{Cases: []casepkg.Case{}, Counts: map[string]int64{}}
	where, args := workspaceWhere(tenant, f)
	rows, err := r.db.Query(ctx, "SELECT c.status,count(*) FROM case_manager.cases c WHERE "+where+" GROUP BY c.status", args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var status string
		var n int64
		if err = rows.Scan(&status, &n); err != nil {
			rows.Close()
			return result, err
		}
		result.Counts[status] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if f.Before != nil {
		n := len(args)
		args = append(args, f.Before.Boosted, f.Before.At, f.Before.ID)
		where += fmt.Sprintf(" AND ((c.boost_reason IS NOT NULL),c.created_at,c.id)<($%d,$%d,$%d)", n+1, n+2, n+3)
	}
	args = append(args, limit+1)
	rows, err = r.db.Query(ctx, fmt.Sprintf(`SELECT c.id,c.tenant_id,c.inbox_id,c.name,c.status,c.outcome,c.type,c.assigned_to,c.snoozed_until,c.boost_reason,c.review_level,c.created_at,c.updated_at,c.created_at+i.sla_days*interval '24 hours' FROM case_manager.cases c JOIN case_manager.inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE %s ORDER BY (c.boost_reason IS NOT NULL) DESC,c.created_at DESC,c.id DESC LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	items, err := collectRows(rows, scanWorkspaceCase)
	if err != nil {
		return result, err
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		result.NextCursor = (casepkg.Cursor{ID: last.ID, At: last.CreatedAt, Boosted: last.BoostReason != nil}).Encode()
	}
	if items != nil {
		result.Cases = items
	}
	return result, nil
}

func (r WorkspaceRepository) Links(ctx context.Context, tenant, id uuid.UUID, kind string, before *casepkg.Cursor, limit int) (casepkg.LinkPage, error) {
	result := casepkg.LinkPage{Items: []json.RawMessage{}}
	tables := map[string]string{"events": "case_events", "decisions": "case_decisions", "screenings": "case_screenings", "files": "case_files"}
	table, ok := tables[kind]
	if !ok {
		return result, casepkg.Invalid("invalid relationship")
	}
	args := []any{tenant, id}
	where := "tenant_id=$1 AND case_id=$2"
	if before != nil {
		args = append(args, before.At, before.ID)
		where += " AND (created_at,id)<($3,$4)"
	}
	args = append(args, limit+1)
	rows, err := r.db.Query(ctx, fmt.Sprintf("SELECT to_jsonb(t),id,created_at FROM case_manager.%s t WHERE %s ORDER BY created_at DESC,id DESC LIMIT $%d", table, where, len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var last casepkg.Cursor
	for rows.Next() {
		var raw json.RawMessage
		var cursor casepkg.Cursor
		if err := rows.Scan(&raw, &cursor.ID, &cursor.At); err != nil {
			return result, err
		}
		if len(result.Items) == limit {
			result.NextCursor = last.Encode()
			break
		}
		result.Items = append(result.Items, raw)
		last = cursor
	}
	return result, rows.Err()
}
