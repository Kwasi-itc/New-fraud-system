package mysqlapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"strconv"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

func executionPageFilter(q url.Values) (mysql.ExecutionPageFilter, error) {
	f := mysql.ExecutionPageFilter{Limit: 100}
	for key, values := range q {
		if len(values) != 1 {
			return f, mysql.ErrExecutionPage
		}
		switch key {
		case "status":
			f.Status = execution.Status(values[0])
			if !mysql.ValidExecutionStatus(f.Status) {
				return f, mysql.ErrExecutionPage
			}
		case "limit", "min_attempt_count":
			n, err := strconv.ParseInt(values[0], 10, 32)
			if err != nil || n < 0 {
				return f, mysql.ErrExecutionPage
			}
			if key == "limit" {
				if n < 1 || n > 500 {
					return f, mysql.ErrExecutionPage
				}
				f.Limit = int(n)
			} else {
				f.MinAttemptCount = int(n)
			}
		case "cursor":
			if len(values[0]) == 0 || len(values[0]) > 2048 {
				return f, mysql.ErrExecutionPage
			}
			raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
			if err != nil {
				return f, mysql.ErrExecutionPage
			}
			var c mysql.ExecutionPageCursor
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err := d.Decode(&c); err != nil {
				return f, mysql.ErrExecutionPage
			}
			if err := d.Decode(new(any)); err != io.EOF {
				return f, mysql.ErrExecutionPage
			}
			// Only our canonical representation is accepted: duplicate fields,
			// alternate key spellings and hidden trailing documents cannot pass.
			canonical, _ := json.Marshal(c)
			if !bytes.Equal(canonical, raw) {
				return f, mysql.ErrExecutionPage
			}
			f.After = &c
		default:
			return f, mysql.ErrExecutionPage
		}
	}
	return f, nil
}

func (a *ExecutionAdapter) Page(ctx context.Context, tenant, parent string, f mysql.ExecutionPageFilter) (map[string]any, error) {
	if a == nil || a.store == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	var page mysql.ExecutionPage
	err := a.store.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) (err error) {
		if parent != "" {
			page, err = r.Scheduled.Page(ctx, tenant, parent, f)
		} else {
			page, err = r.Async.Page(ctx, tenant, f)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	key := "async_decision_executions"
	if parent != "" {
		key = "scheduled_executions"
	}
	out := map[string]any{key: page.Items}
	if page.Next != nil {
		raw, err := json.Marshal(page.Next)
		if err != nil {
			return nil, err
		}
		out["next_cursor"] = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}

func (a *ExecutionAdapter) StatusSummary(ctx context.Context, tenant, parent string) (map[string]any, error) {
	if a == nil || a.store == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	var counts map[execution.Status]int
	err := a.store.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) (err error) {
		if parent != "" {
			counts, err = r.Scheduled.CountByStatus(ctx, tenant, parent)
		} else {
			counts, err = r.Async.CountByStatus(ctx, tenant)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, s := range []execution.Status{"pending", "queued", "running", "completed", "failed", "cancelled"} {
		if _, ok := counts[s]; !ok {
			counts[s] = 0
		}
	}
	return map[string]any{"status_summary": counts}, nil
}
