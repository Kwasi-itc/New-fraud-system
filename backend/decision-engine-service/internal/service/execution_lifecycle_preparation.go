package service

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// PreparedExecutionEvent uses the evaluator's actual publication setting,
// clock and identity generator. Disabled optional publication intentionally
// creates neither event nor job; enabled publication must persist both.
type PreparedExecutionEvent struct{ event *integration.OutboxEvent }

func (s DecisionService) PrepareExecutionEvent(tenant, kind, id, event string, body map[string]any) (PreparedExecutionEvent, error) {
	if !s.publishOutboxEvents {
		return PreparedExecutionEvent{}, nil
	}
	if s.idGen == nil || s.clock == nil || tenant == "" || id == "" || (kind != "scheduled_execution" && kind != "async_decision_execution") {
		return PreparedExecutionEvent{}, ErrExecutionPreparation
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return PreparedExecutionEvent{}, err
	}
	return PreparedExecutionEvent{&integration.OutboxEvent{ID: s.idGen.New().String(), TenantID: tenant, AggregateType: kind, AggregateID: id, EventType: event, Payload: raw, Status: integration.OutboxStatusPending, CreatedAt: s.clock.Now()}}, nil
}
func (p PreparedExecutionEvent) Persist(ctx context.Context, store ports.DecisionMutationStore) error {
	if p.event == nil {
		return nil
	} // Explicit optional-outbox setting, not absent required work.
	if store == nil || store.OutboxEvents() == nil {
		return ErrExecutionPreparation
	}
	rows, err := store.OutboxEvents().CreateMany(ctx, []integration.OutboxEvent{*p.event})
	if err != nil {
		return err
	}
	if len(rows) != 1 || rows[0].ID != p.event.ID {
		return ErrExecutionPreparation
	}
	return store.Enqueue(ctx, ports.JobRequest{Kind: ports.JobOutbox, TenantID: p.event.TenantID, ResourceID: p.event.ID})
}
