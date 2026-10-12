package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// Failure evidence reuses result_body so rollout needs no new application tables or migration.
// Its error member distinguishes it from successful evaluation results.
func (s ExecutionService) failAsyncValidation(ctx context.Context, item execution.AsyncDecisionExecution, validation *payload.Error, now time.Time) error {
	body, err := json.Marshal(struct {
		Error *payload.Error `json:"error"`
	}{validation})
	if err != nil {
		return err
	}
	if s.txManager == nil {
		return fmt.Errorf("async validation failure transaction manager is not configured")
	}
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		repo, ok := store.AsyncDecisionExecutions().(ports.AsyncValidationFailureRepository)
		if !ok {
			return fmt.Errorf("async validation failure repository is not configured")
		}
		callbackStatus := ""
		if item.CallbackURL != "" {
			callbackStatus = "pending"
		}
		if err := repo.MarkValidationFailed(ctx, item.TenantID, item.ID, body, validation.Error(), now, callbackStatus); err != nil {
			return err
		}
		if item.CallbackURL != "" {
			if s.asyncCallbackEnqueuer == nil {
				return fmt.Errorf("async callback enqueuer is not configured")
			}
			if err := s.asyncCallbackEnqueuer.EnqueueTx(ctx, store.RawTx(), item.TenantID, item.ID, nil); err != nil {
				return err
			}
		}
		return s.writeExecutionLifecycleEvents(ctx, store.RawTx(), item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.failed", map[string]any{
			"status": execution.StatusFailed, "attempt_count": item.AttemptCount, "last_error": validation.Error(), "failed_at": now, "validation": validation,
		}, store.OutboxEvents())
	})
}
