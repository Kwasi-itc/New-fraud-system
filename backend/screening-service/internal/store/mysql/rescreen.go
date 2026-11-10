package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/google/uuid"
	"time"
)

func (r jobs) RescreenProgress(ctx context.Context, t, j string) (*ports.RescreenProgress, error) {
	var out ports.RescreenProgress
	var raw []byte
	e := r.q.QueryRowContext(ctx, "SELECT plan,requeued_count,completed FROM "+r.table("rescreen_progress")+" WHERE tenant_id=? AND job_id=?", id(t), id(j)).Scan(&raw, &out.Requeued, &out.Completed)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, normalize(e)
	}
	if e = json.Unmarshal(raw, &out.Plan); e != nil {
		return nil, e
	}
	return &out, nil
}

func (r jobs) AdvanceRescreen(ctx context.Context, item d.DatasetUpdateJob, plan ports.RescreenPlan, limit int) (ports.RescreenProgress, error) {
	var out ports.RescreenProgress
	fence, ok := ctx.Value(workKey{}).(workFence)
	if !ok || fence.Tenant != item.TenantID || fence.ID != item.ID || fence.Kind != "dataset" {
		return out, ports.ErrScreeningBusy
	}
	if limit < 1 || limit > 100 {
		return out, errors.New("invalid rescreen page size")
	}
	raw, e := json.Marshal(plan)
	if e != nil {
		return out, e
	}
	e = r.Store.Run(ctx, func(m ports.MutationStore) error {
		tx := m.(*Store)
		var provider, kind string
		var cutoff time.Time
		if e := tx.q.QueryRowContext(ctx, "SELECT provider,job_type,created_at FROM "+tx.table("dataset_jobs")+" WHERE tenant_id=? AND id=? FOR UPDATE", id(item.TenantID), id(item.ID)).Scan(&provider, &kind, &cutoff); e != nil {
			return e
		}
		if provider != item.Provider || kind != item.JobType {
			return errors.New("dataset job identity changed")
		}
		_, e := tx.q.ExecContext(ctx, "INSERT INTO "+tx.table("rescreen_progress")+"(tenant_id,job_id,cutoff,plan) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE job_id=job_id", id(item.TenantID), id(item.ID), cutoff, mysqlstore.JSON(raw))
		if e != nil {
			return e
		}
		var last mysqlstore.NullUUID
		if e = tx.q.QueryRowContext(ctx, "SELECT last_id,plan,requeued_count,completed,cutoff FROM "+tx.table("rescreen_progress")+" WHERE tenant_id=? AND job_id=? FOR UPDATE", id(item.TenantID), id(item.ID)).Scan(&last, &raw, &out.Requeued, &out.Completed, &cutoff); e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &out.Plan); e != nil {
			return e
		}
		if out.Completed {
			return nil
		}
		if !out.Plan.Changed {
			out.Completed = true
		} else {
			// The primary key supplies a stable keyset. Page all objects up to the job
			// cutoff, then conditionally requeue eligible rows without fetching configs
			// individually or overwriting object attributes from a stale snapshot.
			q := "SELECT o.id,o.status,c.provider,c.enabled,o.deleted_at,c.deleted_at FROM " + tx.table("monitored_objects") + " o JOIN " + tx.table("continuous_configs") + " c ON c.tenant_id=o.tenant_id AND c.id=o.config_id WHERE o.tenant_id=? AND o.created_at<=?"
			args := []any{id(item.TenantID), cutoff}
			if last.Valid {
				q += " AND o.id>?"
				args = append(args, last.UUID)
			}
			q += " ORDER BY o.id LIMIT ?"
			args = append(args, limit)
			rows, e := tx.q.QueryContext(ctx, q, args...)
			if e != nil {
				return e
			}
			eligible := []string{}
			scanned := 0
			for rows.Next() {
				var key, status, provider string
				var enabled bool
				var deleted, configDeleted sql.NullTime
				if e = rows.Scan(uid(&key), &status, &provider, &enabled, &deleted, &configDeleted); e != nil {
					rows.Close()
					return e
				}
				last = mysqlstore.NullUUID{UUID: mysqlstore.UUID(uuid.MustParse(key)), Valid: true}
				scanned++
				if status == "active" && provider == item.Provider && enabled && !deleted.Valid && !configDeleted.Valid {
					eligible = append(eligible, key)
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if len(eligible) > 0 {
				values := []any{id(item.TenantID)}
				for _, key := range eligible {
					values = append(values, id(key))
				}
				ids := placeholders(len(eligible))
				// Lock/reset continuous leases before their objects, matching the normal
				// worker lock order. Unexpired claims retain their fencing token.
				_, e = tx.q.ExecContext(ctx, "UPDATE "+tx.table("work_leases")+" SET attempts=0,last_error='' WHERE tenant_id=? AND kind='continuous' AND id IN("+ids+") AND lease_until<=UTC_TIMESTAMP(6) ORDER BY id", values...)
				if e != nil {
					return e
				}
				values = append(values, item.Provider)
				result, e := tx.q.ExecContext(ctx, "UPDATE "+tx.table("monitored_objects")+" o JOIN "+tx.table("continuous_configs")+" c ON c.tenant_id=o.tenant_id AND c.id=o.config_id SET o.status='pending',o.updated_at=UTC_TIMESTAMP(6) WHERE o.tenant_id=? AND o.id IN("+ids+") AND o.status='active' AND o.deleted_at IS NULL AND c.provider=? AND c.enabled AND c.deleted_at IS NULL", values...)
				if e != nil {
					return e
				}
				n, e := result.RowsAffected()
				if e != nil {
					return e
				}
				out.Requeued += n
			}
			out.Completed = scanned < limit
		}
		var cursor any
		if last.Valid {
			cursor = last.UUID
		}
		_, e = tx.q.ExecContext(ctx, "UPDATE "+tx.table("rescreen_progress")+" SET last_id=?,requeued_count=?,completed=? WHERE tenant_id=? AND job_id=?", cursor, out.Requeued, out.Completed, id(item.TenantID), id(item.ID))
		return e
	})
	return out, e
}
