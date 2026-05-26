package service

import (
	"context"

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

func (s PlatformService) CreateCustomListEntry(ctx context.Context, tenantID, listName, value string) (platform.CustomListEntry, error) {
	item := platform.CustomListEntry{ID: s.idGen.New().String(), TenantID: tenantID, ListName: listName, Value: value, CreatedAt: s.clock.Now()}
	var created platform.CustomListEntry
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.CustomLists().Create(ctx, item)
		return err
	})
	return created, err
}

func (s PlatformService) ListCustomListEntries(ctx context.Context, tenantID, listName string) ([]platform.CustomListEntry, error) {
	return s.customListRepo.ListByName(ctx, tenantID, listName)
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
