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

	if _, err := s.screeningService.CreateScreening(ctx, item.TenantID, screening.SearchRequest{
		Provider:   config.Provider,
		ScenarioID: "",
		ObjectType: item.ObjectType,
		ObjectID:   item.ObjectID,
		Queries:    queries,
	}); err != nil {
		return err
	}

	now := s.clock.Now()
	item.Status = screening.MonitoredObjectStatusActive
	item.UpdatedAt = now
	item.LastScreenedAt = &now
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		_, err := store.MonitoredObjects().Update(ctx, item)
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
