package service

import (
	"context"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type PlatformService struct {
	txManager      ports.TransactionManager
	idGen          ports.IDGenerator
	clock          ports.Clock
	customListRepo ports.CustomListRepository
	recordTagRepo  ports.RecordTagRepository
	riskRepo       ports.RiskSnapshotRepository
	ipFlagRepo     ports.IPFlagRepository
}

func NewPlatformService(txManager ports.TransactionManager, idGen ports.IDGenerator, clock ports.Clock, customListRepo ports.CustomListRepository, recordTagRepo ports.RecordTagRepository, riskRepo ports.RiskSnapshotRepository, ipFlagRepo ports.IPFlagRepository) PlatformService {
	return PlatformService{txManager: txManager, idGen: idGen, clock: clock, customListRepo: customListRepo, recordTagRepo: recordTagRepo, riskRepo: riskRepo, ipFlagRepo: ipFlagRepo}
}

func (s PlatformService) CreateCustomList(ctx context.Context, tenantID, name, description, kind string) (platform.CustomList, error) {
	now := s.clock.Now()
	item := platform.CustomList{ID: s.idGen.New().String(), TenantID: tenantID, Name: name, Description: description, Kind: kind, CreatedAt: now, UpdatedAt: now}
	var created platform.CustomList
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.CustomLists().CreateList(ctx, item)
		return err
	})
	return created, err
}

func (s PlatformService) ListCustomLists(ctx context.Context, tenantID string) ([]platform.CustomList, error) {
	return s.customListRepo.ListLists(ctx, tenantID)
}

func (s PlatformService) GetCustomList(ctx context.Context, tenantID, listID string) (platform.CustomList, error) {
	return s.customListRepo.GetListByID(ctx, tenantID, listID)
}

func (s PlatformService) UpdateCustomList(ctx context.Context, tenantID, listID, name, description, kind string) (platform.CustomList, error) {
	current, err := s.customListRepo.GetListByID(ctx, tenantID, listID)
	if err != nil {
		return platform.CustomList{}, err
	}
	current.Name = name
	current.Description = description
	current.Kind = kind
	current.UpdatedAt = s.clock.Now()
	var updated platform.CustomList
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		repo := store.CustomLists()
		var runErr error
		updated, runErr = repo.UpdateList(ctx, current)
		if runErr != nil {
			return runErr
		}
		return repo.RenameEntriesByListID(ctx, tenantID, listID, current.Name)
	})
	return updated, err
}

func (s PlatformService) DeleteCustomList(ctx context.Context, tenantID, listID string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.CustomLists().DeleteList(ctx, tenantID, listID)
	})
}

func (s PlatformService) CreateCustomListEntry(ctx context.Context, tenantID, listID, value string) (platform.CustomListEntry, error) {
	customList, err := s.customListRepo.GetListByID(ctx, tenantID, listID)
	if err != nil {
		return platform.CustomListEntry{}, err
	}
	item := platform.CustomListEntry{ID: s.idGen.New().String(), TenantID: tenantID, ListID: &customList.ID, ListName: customList.Name, Value: value, CreatedAt: s.clock.Now()}
	var created platform.CustomListEntry
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.CustomLists().Create(ctx, item)
		return runErr
	})
	return created, err
}

func (s PlatformService) UpdateCustomListEntry(ctx context.Context, tenantID, listID, entryID, value string) (platform.CustomListEntry, error) {
	customList, err := s.customListRepo.GetListByID(ctx, tenantID, listID)
	if err != nil {
		return platform.CustomListEntry{}, err
	}
	item := platform.CustomListEntry{
		ID:        entryID,
		TenantID:  tenantID,
		ListID:    &customList.ID,
		ListName:  customList.Name,
		Value:     value,
		CreatedAt: s.clock.Now(),
	}
	var updated platform.CustomListEntry
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.CustomLists().UpdateEntry(ctx, item)
		return runErr
	})
	return updated, err
}

func (s PlatformService) ListCustomListEntries(ctx context.Context, tenantID, listName string) ([]platform.CustomListEntry, error) {
	return s.customListRepo.ListByName(ctx, tenantID, listName)
}

func (s PlatformService) ListCustomListEntriesByListID(ctx context.Context, tenantID, listID string) ([]platform.CustomListEntry, error) {
	return s.customListRepo.ListEntriesByListID(ctx, tenantID, listID)
}

func (s PlatformService) DeleteCustomListEntry(ctx context.Context, tenantID, listID, entryID string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.CustomLists().DeleteEntry(ctx, tenantID, listID, entryID)
	})
}

func (s PlatformService) ImportCustomListEntries(ctx context.Context, tenantID, listID string, values []string) (int, error) {
	customList, err := s.customListRepo.GetListByID(ctx, tenantID, listID)
	if err != nil {
		return 0, err
	}

	seen := make(map[string]struct{}, len(values))
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		filtered = append(filtered, trimmed)
	}

	if len(filtered) == 0 {
		return 0, nil
	}

	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		repo := store.CustomLists()
		for _, value := range filtered {
			item := platform.CustomListEntry{
				ID:        s.idGen.New().String(),
				TenantID:  tenantID,
				ListID:    &customList.ID,
				ListName:  customList.Name,
				Value:     value,
				CreatedAt: s.clock.Now(),
			}
			if _, createErr := repo.Create(ctx, item); createErr != nil {
				return createErr
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	return len(filtered), nil
}

func (s PlatformService) CreateRecordTag(ctx context.Context, tenantID, objectType, objectID, tag string) (platform.RecordTag, error) {
	item := platform.RecordTag{ID: s.idGen.New().String(), TenantID: tenantID, ObjectType: objectType, ObjectID: objectID, Tag: tag, CreatedAt: s.clock.Now()}
	var created platform.RecordTag
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.RecordTags().Create(ctx, item)
		return err
	})
	return created, err
}

func (s PlatformService) ListRecordTags(ctx context.Context, tenantID, objectType, objectID string) ([]platform.RecordTag, error) {
	return s.recordTagRepo.ListByObject(ctx, tenantID, objectType, objectID)
}

func (s PlatformService) CreateRiskSnapshot(ctx context.Context, tenantID, objectType, objectID, riskLevel string) (platform.RiskSnapshot, error) {
	item := platform.RiskSnapshot{ID: s.idGen.New().String(), TenantID: tenantID, ObjectType: objectType, ObjectID: objectID, RiskLevel: riskLevel, CreatedAt: s.clock.Now()}
	var created platform.RiskSnapshot
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.RiskSnapshots().Create(ctx, item)
		return err
	})
	return created, err
}

func (s PlatformService) CreateIPFlag(ctx context.Context, tenantID, ipAddress, flag string) (platform.IPFlag, error) {
	item := platform.IPFlag{ID: s.idGen.New().String(), TenantID: tenantID, IPAddress: ipAddress, Flag: flag, CreatedAt: s.clock.Now()}
	var created platform.IPFlag
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.IPFlags().Create(ctx, item)
		return err
	})
	return created, err
}

func (s PlatformService) ListIPFlags(ctx context.Context, tenantID, ipAddress string) ([]platform.IPFlag, error) {
	return s.ipFlagRepo.ListByIP(ctx, tenantID, ipAddress)
}
