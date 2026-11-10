package service

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

func (s DatasetUpdateWorkerService) processResumable(ctx context.Context, item screening.DatasetUpdateJob, repo ports.ResumableDatasetJobs) error {
	saved, err := repo.RescreenProgress(ctx, item.TenantID, item.ID)
	if err != nil {
		return err
	}
	plan := ports.RescreenPlan{Changed: true, NextCursor: item.Cursor}
	if saved != nil {
		plan = saved.Plan
	} else if item.JobType == "dataset_delta_rescreen" {
		delta, e := s.provider.GetDatasetDelta(ctx, item.Provider, item.Cursor)
		if e != nil {
			return s.failJob(ctx, item, e.Error())
		}
		plan = ports.RescreenPlan{Delta: delta.RawPayload, Changed: delta.Changed, NextCursor: item.Cursor}
		if len(plan.Delta) == 0 {
			plan.Delta = json.RawMessage(`{}`)
		}
		if delta.NextCursor != "" {
			plan.NextCursor = delta.NextCursor
		}
	}
	progress, err := repo.AdvanceRescreen(ctx, item, plan, 50)
	if err != nil {
		return err
	} // Leave processing recoverable; do not discard checkpoint.
	now := s.clock.Now()
	item.UpdatedAt = now
	item.LastError = ""
	item.ResultJSON, err = json.Marshal(map[string]any{"job_type": item.JobType, "provider": item.Provider, "requeued_objects": progress.Requeued, "has_more": !progress.Completed, "changed": progress.Plan.Changed, "delta": progress.Plan.Delta})
	if err != nil {
		return err
	}
	item.Status = screening.DatasetUpdateJobStatusPending
	if progress.Completed {
		item.Status = screening.DatasetUpdateJobStatusCompleted
		item.CompletedAt = &now
		item.Cursor = progress.Plan.NextCursor
	}
	return s.txManager.Run(ctx, func(m ports.MutationStore) error { _, e := m.DatasetUpdateJobs().Update(ctx, item); return e })
}
