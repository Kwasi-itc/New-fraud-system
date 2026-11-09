package mysqlworkflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/google/uuid"
	"time"
)

// ReceiptReceiver exposes the existing case owner's transactional workflow
// implementation to a separately authenticated, fixed-tenant development host.
// It never derives a service principal from dispatch JSON or user headers.
type ReceiptReceiver struct {
	owner  service.WorkflowReceiptService
	tenant uuid.UUID
}
type receiverIDs struct{}

func (receiverIDs) New() uuid.UUID { return uuid.New() }

type receiverClock struct{}

func (receiverClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
func NewReceiptReceiver(db *sql.DB, database string, tenant uuid.UUID) (*ReceiptReceiver, error) {
	if tenant == uuid.Nil {
		return nil, errors.New("fixed workflow receiver tenant required")
	}
	repo, err := store.NewWorkflowStore(db, database, receiverIDs{}, receiverClock{})
	if err != nil {
		return nil, err
	}
	owner, err := service.NewWorkflowReceiptService(repo, receiverClock{})
	if err != nil {
		return nil, err
	}
	return &ReceiptReceiver{owner, tenant}, nil
}
func (s *ReceiptReceiver) Receive(ctx context.Context, raw []byte, reconcile bool) (json.RawMessage, error) {
	var request service.WorkflowDispatchRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	ctx = access.WithPrincipal(ctx, access.Principal{Kind: access.Service, Subject: "local-decision-delivery", ServiceTenants: []uuid.UUID{s.tenant}})
	var out service.WorkflowDispatchReceipt
	var err error
	if reconcile {
		out, err = s.owner.ReconcileWorkflowReceipt(ctx, request)
	} else {
		out, err = s.owner.DeliverWorkflowReceipt(ctx, request)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
