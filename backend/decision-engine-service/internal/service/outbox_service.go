package service

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type OutboxService struct {
	repo ports.OutboxEventRepository
}

func NewOutboxService(repo ports.OutboxEventRepository) OutboxService {
	return OutboxService{repo: repo}
}

func (s OutboxService) ListByTenant(ctx context.Context, tenantID string, limit int) ([]integration.OutboxEvent, error) {
	return s.repo.ListByTenant(ctx, tenantID, limit)
}
