package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"sort"
)

// PreparePublicationIndexes uses only owner APIs and holds no database locks.
// A failed multi-job start can leave earlier jobs accepted by the owner. Callers
// must reconcile through status; this is not an atomic publication or DDL result.
func PreparePublicationIndexes(ctx context.Context, parent scenario.Scenario, it scenario.Iteration, rules []scenario.Rule, model ports.TenantModel, owner ports.IndexPreparationProvider, start bool, limits ExecutionPreparationLimits) (scenario.PublicationPreparationStatus, error) {
	maxJobs := min(limits.MaxItems, 1000)
	if owner == nil || maxJobs < 1 || maxJobs > 1000 {
		return scenario.PublicationPreparationStatus{}, ErrExecutionPreparation
	}
	validation, err := ValidateIterationSnapshot(parent, it, rules, model, limits)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	if !validation.Valid {
		return scenario.PublicationPreparationStatus{}, ErrExecutionPreparation
	}
	tables, err := collectReferencedTables(model, parent.TriggerObjectType, it.TriggerFormula, rules)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	ids := make([]string, 0, len(tables))
	for name := range tables {
		table, ok := model.Tables[name]
		if !ok || table.ID == "" || table.Archived {
			return scenario.PublicationPreparationStatus{}, ErrExecutionPreparation
		}
		ids = append(ids, table.ID)
	}
	if len(ids) > 100 {
		return scenario.PublicationPreparationStatus{}, ErrExecutionPreparationLimit
	}
	sort.Strings(ids)
	required, err := collectIndexRequirements(model, parent.TriggerObjectType, it.TriggerFormula, rules)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	if len(required) > maxJobs {
		return scenario.PublicationPreparationStatus{}, ErrExecutionPreparationLimit
	}
	read := func() ([]ports.ManagedIndexJob, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		jobs, err := owner.ListIndexJobsForTables(ctx, parent.TenantID, ids, maxJobs)
		if err != nil {
			return nil, err
		}
		if len(jobs) > maxJobs {
			return nil, ErrExecutionPreparationLimit
		}
		if jobs == nil {
			return nil, fmt.Errorf("missing complete index preparation result")
		}
		encoded, err := json.Marshal(jobs)
		if err != nil {
			return nil, err
		}
		if len(encoded) > limits.MaxBytes {
			return nil, ErrExecutionPreparationLimit
		}
		seen := map[string]bool{}
		for _, job := range jobs {
			if _, ok := tables[job.TableName]; !ok || job.TenantID != parent.TenantID || job.TableID != model.Tables[job.TableName].ID || job.ID == "" || seen[job.ID] {
				return nil, fmt.Errorf("invalid index preparation owner result")
			}
			seen[job.ID] = true
		}
		return jobs, nil
	}
	jobs, err := read()
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	if start {
		for _, r := range required {
			if err := ctx.Err(); err != nil {
				return scenario.PublicationPreparationStatus{}, err
			}
			if hasIndexJobForRequirement(jobs, r) {
				continue
			}
			if _, err := owner.CreateIndexJob(ctx, parent.TenantID, r.TableID, "search", r.Columns, "scenario_publication_preparation"); err != nil {
				return scenario.PublicationPreparationStatus{}, err
			}
		}
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return scenario.PublicationPreparationStatus{}, err
			}
			if job.Status == "failed" {
				if err := owner.RetryIndexJob(ctx, job.ID); err != nil {
					return scenario.PublicationPreparationStatus{}, err
				}
			}
		}
		jobs, err = read()
		if err != nil {
			return scenario.PublicationPreparationStatus{}, err
		}
	}
	return publicationPreparationStatus(parent.ID, it.ID, required, jobs), nil
}
