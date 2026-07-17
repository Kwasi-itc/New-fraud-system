package service

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
)

type DatasetUpdateWorkerService struct {
	txManager      ports.TransactionManager
	clock          ports.Clock
	datasetJobRepo ports.DatasetUpdateJobRepository
	continuousRepo ports.ContinuousConfigRepository
	monitoredRepo  ports.MonitoredObjectRepository
	provider       ports.ScreeningProvider
	monitoredEnq   riverjobs.MonitoredObjectEnqueuer
}

func NewDatasetUpdateWorkerService(
	txManager ports.TransactionManager,
	clock ports.Clock,
	datasetJobRepo ports.DatasetUpdateJobRepository,
	continuousRepo ports.ContinuousConfigRepository,
	monitoredRepo ports.MonitoredObjectRepository,
	provider ports.ScreeningProvider,
	monitoredEnq riverjobs.MonitoredObjectEnqueuer,
) DatasetUpdateWorkerService {
	if monitoredEnq == nil {
		monitoredEnq = riverjobs.NoopMonitoredObjectEnqueuer{}
	}
	return DatasetUpdateWorkerService{
		txManager:      txManager,
		clock:          clock,
		datasetJobRepo: datasetJobRepo,
		continuousRepo: continuousRepo,
		monitoredRepo:  monitoredRepo,
		provider:       provider,
		monitoredEnq:   monitoredEnq,
	}
}

func (s DatasetUpdateWorkerService) ProcessPendingJobs(ctx context.Context, limit int) error {
	items, err := s.datasetJobRepo.ListByStatus(ctx, screening.DatasetUpdateJobStatusPending, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.processJob(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s DatasetUpdateWorkerService) RunJob(ctx context.Context, tenantID, jobID string) error {
	item, err := s.datasetJobRepo.GetByID(ctx, tenantID, jobID)
	if err != nil {
		return err
	}
	if item.Status != screening.DatasetUpdateJobStatusPending {
		return nil
	}
	return s.processJob(ctx, item)
}

func (s DatasetUpdateWorkerService) processJob(ctx context.Context, item screening.DatasetUpdateJob) error {
	now := s.clock.Now()
	item.Status = screening.DatasetUpdateJobStatusProcessing
	item.AttemptCount++
	item.UpdatedAt = now
	item.StartedAt = &now
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		_, err := store.DatasetUpdateJobs().Update(ctx, item)
		return err
	}); err != nil {
		return err
	}

	var result []byte
	var err error
	switch item.JobType {
	case "catalog_sync":
		var catalog screening.DatasetCatalog
		catalog, err = s.provider.GetCatalog(ctx, item.Provider)
		if err == nil {
			result = catalog.RawPayload
		}
	case "dataset_delta_sync", "dataset_delta_rescreen":
		result, item.Cursor, err = s.syncDatasetDelta(ctx, item)
	case "rescreen_monitored_objects":
		result, err = s.rescreenMonitoredObjects(ctx, item)
	default:
		var freshness screening.DatasetFreshness
		freshness, err = s.provider.GetFreshness(ctx, item.Provider)
		if err == nil {
			result = freshness.RawPayload
		}
	}
	if err != nil {
		return s.failJob(ctx, item, err.Error())
	}

	finished := s.clock.Now()
	item.Status = screening.DatasetUpdateJobStatusCompleted
	item.ResultJSON = result
	item.LastError = ""
	item.UpdatedAt = finished
	item.CompletedAt = &finished
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		_, err := store.DatasetUpdateJobs().Update(ctx, item)
		return err
	})
}

func (s DatasetUpdateWorkerService) rescreenMonitoredObjects(ctx context.Context, item screening.DatasetUpdateJob) ([]byte, error) {
	objects, err := s.monitoredRepo.ListByTenantAndStatus(ctx, item.TenantID, screening.MonitoredObjectStatusActive, 500)
	if err != nil {
		return nil, err
	}

	requeued := 0
	now := s.clock.Now()
	for _, object := range objects {
		config, err := s.continuousRepo.GetByID(ctx, object.TenantID, object.ConfigID)
		if err != nil {
			return nil, err
		}
		if config.Provider != item.Provider {
			continue
		}
		object.Status = screening.MonitoredObjectStatusPending
		object.UpdatedAt = now
		if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
			updated, updateErr := store.MonitoredObjects().Update(ctx, object)
			if updateErr != nil {
				return updateErr
			}
			return s.monitoredEnq.EnqueueTx(ctx, store.RawTx(), updated.TenantID, updated.ID, nil)
		}); err != nil {
			return nil, err
		}
		requeued++
	}

	return json.Marshal(map[string]any{
		"job_type":         item.JobType,
		"provider":         item.Provider,
		"requeued_objects": requeued,
	})
}

func (s DatasetUpdateWorkerService) syncDatasetDelta(ctx context.Context, item screening.DatasetUpdateJob) ([]byte, string, error) {
	delta, err := s.provider.GetDatasetDelta(ctx, item.Provider, item.Cursor)
	if err != nil {
		return nil, item.Cursor, err
	}
	nextCursor := item.Cursor
	if delta.NextCursor != "" {
		nextCursor = delta.NextCursor
	}

	if item.JobType == "dataset_delta_rescreen" && delta.Changed {
		requeuePayload, err := s.rescreenMonitoredObjects(ctx, item)
		if err != nil {
			return nil, item.Cursor, err
		}
		result, marshalErr := json.Marshal(map[string]any{
			"delta":            json.RawMessage(delta.RawPayload),
			"next_cursor":      delta.NextCursor,
			"has_more":         delta.HasMore,
			"changed":          delta.Changed,
			"rescreen_summary": json.RawMessage(requeuePayload),
		})
		if marshalErr != nil {
			return nil, item.Cursor, marshalErr
		}
		return result, nextCursor, nil
	}

	result := delta.RawPayload
	if len(result) == 0 {
		var marshalErr error
		result, marshalErr = json.Marshal(map[string]any{
			"next_cursor": delta.NextCursor,
			"has_more":    delta.HasMore,
			"changed":     delta.Changed,
		})
		if marshalErr != nil {
			return nil, item.Cursor, marshalErr
		}
	}
	return result, nextCursor, nil
}

func (s DatasetUpdateWorkerService) failJob(ctx context.Context, item screening.DatasetUpdateJob, message string) error {
	now := s.clock.Now()
	item.Status = screening.DatasetUpdateJobStatusFailed
	item.LastError = message
	item.UpdatedAt = now
	item.CompletedAt = nil
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		_, err := store.DatasetUpdateJobs().Update(ctx, item)
		return err
	})
}
