package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrExecutionPage = errors.New("invalid execution page, filter or cursor")

// Collection projections deliberately exclude request/result bodies, callback
// destinations and error text. Fetch an authorized individual owner for detail.
type ExecutionSummary struct {
	ID            string           `json:"id"`
	TenantID      string           `json:"tenant_id"`
	ScenarioID    string           `json:"scenario_id,omitempty"`
	Status        execution.Status `json:"status"`
	Generation    int              `json:"generation"`
	AttemptCount  int              `json:"attempt_count"`
	MaxAttempts   int              `json:"max_attempts"`
	CreatedAt     time.Time        `json:"created_at"`
	ScheduledFor  *time.Time       `json:"scheduled_for,omitempty"`
	NextAttemptAt *time.Time       `json:"next_attempt_at,omitempty"`
	CompletedAt   *time.Time       `json:"completed_at,omitempty"`
	FailedAt      *time.Time       `json:"failed_at,omitempty"`
	CancelledAt   *time.Time       `json:"cancelled_at,omitempty"`
}

type ExecutionPageFilter struct {
	Status          execution.Status
	MinAttemptCount int
	Limit           int
	After           *ExecutionPageCursor
}

// A cursor binds both scope and filters. It is not authority: every page is
// authorized anew. Descending order exactly follows the owner's history index.
type ExecutionPageCursor struct {
	TenantID        uuid.UUID        `json:"tenant"`
	ScenarioID      uuid.UUID        `json:"scenario"`
	Status          execution.Status `json:"status"`
	MinAttemptCount int              `json:"min_attempt_count"`
	CreatedAt       time.Time        `json:"created_at"`
	ScheduledFor    time.Time        `json:"scheduled_for"`
	ID              uuid.UUID        `json:"id"`
}

type ExecutionPage struct {
	Items []ExecutionSummary
	Next  *ExecutionPageCursor
}

func ValidExecutionStatus(s execution.Status) bool {
	switch s {
	case execution.StatusPending, execution.StatusQueued, execution.StatusRunning, execution.StatusCompleted, execution.StatusFailed, execution.Status("cancelled"):
		return true
	}
	return false
}

func (f ExecutionPageFilter) validate(tenant, parent uuid.UUID, maxRows int) error {
	if f.Limit < 1 || f.Limit > min(maxRows, 500) || f.MinAttemptCount < 0 || f.MinAttemptCount > math.MaxInt32 || f.Status != "" && !ValidExecutionStatus(f.Status) {
		return ErrExecutionPage
	}
	if c := f.After; c != nil {
		if c.TenantID != tenant || c.ScenarioID != parent || c.Status != f.Status || c.MinAttemptCount != f.MinAttemptCount || c.ID == uuid.Nil {
			return ErrExecutionPage
		}
		if _, err := (mysqlstore.DateTime{Time: c.CreatedAt}).Value(); err != nil {
			return ErrExecutionPage
		}
		if parent != uuid.Nil {
			if _, err := (mysqlstore.DateTime{Time: c.ScheduledFor}).Value(); err != nil {
				return ErrExecutionPage
			}
		} else if !c.ScheduledFor.IsZero() {
			return ErrExecutionPage
		}
	}
	return nil
}

func (r ScheduledExecutionRepository) Page(ctx context.Context, tenant, parent string, f ExecutionPageFilter) (ExecutionPage, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return ExecutionPage{}, err
	}
	return r.executionRepository.page(ctx, uuid.MustParse(parent), f, args)
}
func (r AsyncDecisionExecutionRepository) Page(ctx context.Context, tenant string, f ExecutionPageFilter) (ExecutionPage, error) {
	args, err := r.ids(tenant)
	if err != nil {
		return ExecutionPage{}, err
	}
	return r.executionRepository.page(ctx, uuid.Nil, f, args)
}
func (r executionRepository) page(ctx context.Context, parent uuid.UUID, f ExecutionPageFilter, args []any) (ExecutionPage, error) {
	if err := f.validate(r.tenant, parent, r.maxRows); err != nil {
		return ExecutionPage{}, err
	}
	scheduled := parent != uuid.Nil
	query := "SELECT e.id,e.status,e.generation,e.attempt_count,e.max_attempts,e.created_at,e.next_attempt_at,e.failed_at,e.cancelled_at,"
	where := " WHERE e.tenant_id=?"
	order := "e.created_at DESC,e.id DESC"
	if scheduled {
		query += "e.scheduled_for,NULL FROM " + r.schema + ".decision_engine_scheduled_executions e"
		where += " AND e.scenario_id=?"
		order = "e.scheduled_for DESC," + order
	} else {
		query += "NULL,e.completed_at FROM " + r.schema + ".decision_engine_async_executions e"
	}
	if f.Status != "" {
		where += " AND e.status=?"
		args = append(args, string(f.Status))
	}
	if f.MinAttemptCount > 0 {
		where += " AND e.attempt_count>=?"
		args = append(args, f.MinAttemptCount)
	}
	if c := f.After; c != nil {
		created := mysqlstore.DateTime{Time: c.CreatedAt}
		id := mysqlstore.UUID(c.ID)
		if scheduled {
			at := mysqlstore.DateTime{Time: c.ScheduledFor}
			where += " AND(e.scheduled_for<? OR(e.scheduled_for=? AND(e.created_at<? OR(e.created_at=? AND e.id<?))))"
			args = append(args, at, at, created, created, id)
		} else {
			where += " AND(e.created_at<? OR(e.created_at=? AND e.id<?))"
			args = append(args, created, created, id)
		}
	}
	rows, err := r.tx.QueryContext(ctx, query+where+" ORDER BY "+order+" LIMIT ?", append(args, f.Limit+1)...)
	if err != nil {
		return ExecutionPage{}, err
	}
	defer rows.Close()
	out := ExecutionPage{Items: make([]ExecutionSummary, 0, f.Limit)}
	used := 0
	for rows.Next() {
		var id mysqlstore.UUID
		var created mysqlstore.DateTime
		var next, failed, cancelled, at, completed mysqlstore.NullDateTime
		v := ExecutionSummary{TenantID: r.tenant.String()}
		if err := rows.Scan(&id, &v.Status, &v.Generation, &v.AttemptCount, &v.MaxAttempts, &created, &next, &failed, &cancelled, &at, &completed); err != nil {
			return ExecutionPage{}, err
		}
		if len(out.Items) == f.Limit {
			last := out.Items[len(out.Items)-1]
			c := &ExecutionPageCursor{TenantID: r.tenant, ScenarioID: parent, Status: f.Status, MinAttemptCount: f.MinAttemptCount, CreatedAt: last.CreatedAt, ID: uuid.MustParse(last.ID)}
			if last.ScheduledFor != nil {
				c.ScheduledFor = *last.ScheduledFor
			}
			out.Next = c
			break
		}
		v.ID, v.CreatedAt = uuid.UUID(id).String(), created.Time
		if scheduled {
			v.ScenarioID = parent.String()
		}
		v.NextAttemptAt, v.FailedAt, v.CancelledAt, v.ScheduledFor, v.CompletedAt = datePointer(next), datePointer(failed), datePointer(cancelled), datePointer(at), datePointer(completed)
		encoded, err := json.Marshal(v)
		if err != nil {
			return ExecutionPage{}, err
		}
		if len(encoded) > r.maxBytes-used {
			return ExecutionPage{}, ErrProjectionLimit
		}
		used += len(encoded)
		out.Items = append(out.Items, v)
	}
	if err := rows.Err(); err != nil {
		return ExecutionPage{}, err
	}
	return out, nil
}
