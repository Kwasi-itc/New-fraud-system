package service

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type SnoozeService struct {
	txManager    ports.TransactionManager
	idGen        ports.IDGenerator
	clock        ports.Clock
	scenarioRepo ports.ScenarioRepository
	snoozeRepo   ports.RuleSnoozeRepository
}

func NewSnoozeService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	scenarioRepo ports.ScenarioRepository,
	snoozeRepo ports.RuleSnoozeRepository,
) SnoozeService {
	return SnoozeService{
		txManager:    txManager,
		idGen:        idGen,
		clock:        clock,
		scenarioRepo: scenarioRepo,
		snoozeRepo:   snoozeRepo,
	}
}

func (s SnoozeService) Create(
	ctx context.Context,
	tenantID, scenarioID, objectType, objectID, snoozeGroupID string,
	expiresAt time.Time,
) (snooze.RuleSnooze, error) {
	if _, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID); err != nil {
		return snooze.RuleSnooze{}, err
	}
	now := s.clock.Now()
	item := snooze.RuleSnooze{
		ID:            s.idGen.New().String(),
		TenantID:      tenantID,
		ScenarioID:    scenarioID,
		ObjectType:    objectType,
		ObjectID:      objectID,
		SnoozeGroupID: snoozeGroupID,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
	}
	if err := item.Validate(); err != nil {
		return snooze.RuleSnooze{}, err
	}

	var created snooze.RuleSnooze
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.RuleSnoozes().Create(ctx, item)
		return err
	})
	return created, err
}

func (s SnoozeService) ListActive(ctx context.Context, tenantID, scenarioID, objectType, objectID string) ([]snooze.RuleSnooze, error) {
	return s.snoozeRepo.ListActive(ctx, tenantID, scenarioID, objectType, objectID, s.clock.Now())
}
