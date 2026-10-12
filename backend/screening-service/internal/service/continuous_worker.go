package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type ContinuousWorkerService struct {
	txManager        ports.TransactionManager
	clock            ports.Clock
	continuousRepo   ports.ContinuousConfigRepository
	monitoredObjRepo ports.MonitoredObjectRepository
	tenantReader     ports.TenantDataReader
	screeningService ScreeningService
}

func NewContinuousWorkerService(
	txManager ports.TransactionManager,
	clock ports.Clock,
	continuousRepo ports.ContinuousConfigRepository,
	monitoredObjRepo ports.MonitoredObjectRepository,
	tenantReader ports.TenantDataReader,
	screeningService ScreeningService,
) ContinuousWorkerService {
	return ContinuousWorkerService{
		txManager:        txManager,
		clock:            clock,
		continuousRepo:   continuousRepo,
		monitoredObjRepo: monitoredObjRepo,
		tenantReader:     tenantReader,
		screeningService: screeningService,
	}
}

func (s ContinuousWorkerService) ProcessPendingMonitoredObjects(ctx context.Context, limit int) error {
	items, err := s.monitoredObjRepo.ListByStatus(ctx, screening.MonitoredObjectStatusPending, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.processMonitoredObject(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s ContinuousWorkerService) RunMonitoredObject(ctx context.Context, tenantID, monitoredObjectID string) error {
	item, err := s.monitoredObjRepo.GetByID(ctx, tenantID, monitoredObjectID)
	if err != nil {
		return err
	}
	if item.Status != screening.MonitoredObjectStatusPending {
		return nil
	}
	return s.processMonitoredObject(ctx, item)
}

func (s ContinuousWorkerService) processMonitoredObject(ctx context.Context, item screening.MonitoredObject) error {
	config, err := s.continuousRepo.GetByID(ctx, item.TenantID, item.ConfigID)
	if err != nil {
		return err
	}

	record, err := s.tenantReader.GetRecord(ctx, item.TenantID, item.ObjectType, item.ObjectID)
	if err != nil {
		return err
	}

	queries, err := buildQueries(config.FieldMapJSON, record.Fields)
	if err != nil {
		return err
	}
	if len(queries) == 0 {
		return fmt.Errorf("no screening queries could be derived for monitored object %s", item.ID)
	}

	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		current, err := store.MonitoredObjects().Lock(ctx, item.TenantID, item.ID)
		if err != nil {
			return err
		}
		if current.Status != screening.MonitoredObjectStatusPending {
			return nil
		}
		if !current.UpdatedAt.Equal(item.UpdatedAt) || current.ConfigID != item.ConfigID {
			return fmt.Errorf("monitored object changed; retry current run")
		}
		currentConfig, err := store.ContinuousConfigs().GetByID(ctx, item.TenantID, item.ConfigID)
		if err != nil {
			return err
		}
		if !currentConfig.Enabled {
			return fmt.Errorf("continuous configuration is disabled")
		}
		if !currentConfig.UpdatedAt.Equal(config.UpdatedAt) {
			return fmt.Errorf("continuous configuration changed; retry current run")
		}
		created, err := s.screeningService.createScreening(ctx, store, item.TenantID, screening.SearchRequest{Provider: config.Provider, ObjectType: item.ObjectType, ObjectID: item.ObjectID, Queries: queries})
		if err != nil {
			return err
		}
		now := s.clock.Now()
		if err := store.Screenings().CreateContinuousRun(ctx, item.TenantID, created.ID, item.ConfigID, item.ID, now); err != nil {
			return err
		}
		current.Status = screening.MonitoredObjectStatusActive
		current.UpdatedAt = now
		current.LastScreenedAt = &now
		_, err = store.MonitoredObjects().Update(ctx, current)
		return err
	})
}

func buildQueries(fieldMapJSON []byte, fields map[string]any) ([]screening.SearchQuery, error) {
	if len(fieldMapJSON) == 0 {
		return nil, nil
	}
	var fieldMap map[string]string
	if err := json.Unmarshal(fieldMapJSON, &fieldMap); err != nil {
		return nil, fmt.Errorf("decode field map: %w", err)
	}
	queries := make([]screening.SearchQuery, 0, len(fieldMap))
	for fieldName, queryType := range fieldMap {
		raw, ok := fields[fieldName]
		if !ok {
			continue
		}
		value := fmt.Sprint(raw)
		if value == "" {
			continue
		}
		queries = append(queries, screening.SearchQuery{
			Name: value,
			Type: queryType,
		})
	}
	return queries, nil
}
