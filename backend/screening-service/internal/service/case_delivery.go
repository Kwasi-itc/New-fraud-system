package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type CaseDeliveryService struct {
	repo      ports.CaseDeliveryRepository
	publisher ports.CasePublisher
	logger    *slog.Logger
}

func NewCaseDeliveryService(repo ports.CaseDeliveryRepository, publisher ports.CasePublisher, logger *slog.Logger) CaseDeliveryService {
	return CaseDeliveryService{repo: repo, publisher: publisher, logger: logger}
}
func (s CaseDeliveryService) RunBatch(ctx context.Context, limit int) error {
	if limit < 1 || limit > 1000 {
		return fmt.Errorf("case delivery batch limit must be 1..1000")
	}
	for i := 0; i < limit; i++ {
		item, err := s.repo.Claim(ctx)
		if err != nil {
			return err
		}
		if item == nil {
			return nil
		}
		// No database transaction/lock is held across the network request.
		deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.deliver(deliveryCtx, *item)
		cancel()
		finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		finishErr := s.repo.Finish(finishCtx, *item, err)
		stop()
		if finishErr != nil {
			return finishErr
		}
		if err != nil {
			s.logger.Warn("case callback delivery failed", "event_id", item.ID, "attempt", item.Attempts, "error", err)
		}
	}
	return nil
}
func (s CaseDeliveryService) deliver(ctx context.Context, item ports.CaseEvent) error {
	switch item.Kind {
	case "reviewed":
		var cmd ports.ScreeningReviewedCommand
		if err := json.Unmarshal(item.Payload, &cmd); err != nil {
			return err
		}
		return s.publisher.PublishScreeningReviewed(ctx, cmd)
	case "evidence-uploaded":
		var cmd ports.ScreeningEvidenceUploadedCommand
		if err := json.Unmarshal(item.Payload, &cmd); err != nil {
			return err
		}
		return s.publisher.PublishScreeningEvidenceUploaded(ctx, cmd)
	default:
		return fmt.Errorf("unknown case callback kind %q", item.Kind)
	}
}
