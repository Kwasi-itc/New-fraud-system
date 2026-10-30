package mysqlexecution

import (
	"context"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

var ErrDispatchUncertain = errors.New("external dispatch outcome unknown; reconcile original intent")
var ErrDispatchRejected = errors.New("provider definitively rejected dispatch")

// DispatchWorker handles workflow, screening, scoring, outbox or callback using
// an explicitly constructed scoped store. Its provider must implement the new
// receipt contract; existing legacy send-only clients are deliberately not
// adapted. Real provider registration and runtime activation remain blocked.
type DispatchWorker struct {
	store         *mysql.DispatchStore
	provider      ports.ReceiptDispatcher
	poller        *workers.DeliveryPoller
	callbackRetry time.Duration
}

func NewDispatchWorker(store *mysql.DispatchStore, provider ports.ReceiptDispatcher, policy EvaluationWorkerPolicy) (*DispatchWorker, error) {
	lease, err := store.LeaseDuration()
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	poller, err := newEvaluationPoller(lease, policy)
	if err != nil {
		return nil, err
	}
	return &DispatchWorker{store, provider, poller, policy.DomainRetryBackoff}, nil
}
func (w *DispatchWorker) Run(ctx context.Context, report func(context.Context, error) error) error {
	if w == nil || w.poller == nil {
		return mysql.ErrDecisionAuthorizationUnavailable
	}
	return w.poller.Run(ctx, w.Claim, report)
}
func (w *DispatchWorker) Claim(ctx context.Context) (*workers.DeliveryTask, error) {
	if w == nil || w.store == nil || w.provider == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	lease, err := w.store.Claim(ctx)
	if err != nil || lease == nil {
		return nil, err
	}
	return &workers.DeliveryTask{Heartbeat: func(ctx context.Context) error { return w.store.Heartbeat(ctx, *lease) }, Execute: func(ctx context.Context) error {
		intent, err := w.store.Prepare(ctx, *lease)
		if err != nil {
			return err
		}
		// A committed send-intent exists before any network call. If this process
		// dies afterwards, rescue claims reconcile it rather than blind resend.
		receipt, err := resolveDispatch(ctx, w.provider, intent)
		if err != nil {
			return err
		}
		err = w.store.Complete(ctx, *lease, intent.Request, receipt, w.callbackRetry)
		if ambiguousTransaction(err) {
			committed, readErr := w.store.CompletionCommitted(ctx, *lease, receipt)
			if readErr != nil || !committed {
				return errors.Join(err, readErr)
			}
			err = nil
		}
		if err != nil {
			return err
		}
		if receipt.Outcome == ports.DispatchRejected {
			return ErrDispatchRejected
		}
		return nil
	}}, nil
}

func resolveDispatch(ctx context.Context, provider ports.ReceiptDispatcher, intent mysql.DispatchIntent) (ports.DispatchReceipt, error) {
	if provider == nil {
		return ports.DispatchReceipt{}, mysql.ErrOwnerContractUnavailable
	}
	request := intent.Request
	check := func(receipt ports.DispatchReceipt, err error) (ports.DispatchReceipt, error) {
		if err != nil {
			return ports.DispatchReceipt{}, errors.Join(ErrDispatchUncertain, err)
		}
		if err := mysql.ValidateDispatchReceipt(request, receipt); err != nil {
			return ports.DispatchReceipt{}, err
		}
		return receipt, nil
	}
	if err := ctx.Err(); err != nil {
		return ports.DispatchReceipt{}, context.Cause(ctx)
	}
	if intent.Resumed {
		receipt, err := check(provider.Reconcile(ctx, request))
		if err != nil {
			return ports.DispatchReceipt{}, err
		}
		switch receipt.Outcome {
		case ports.DispatchAccepted, ports.DispatchRejected:
			return receipt, nil
		case ports.DispatchUnknown:
			return ports.DispatchReceipt{}, ErrDispatchUncertain
		case ports.DispatchNotFound: // Explicit safe idempotent resend, SAME key/body.
		}
	}
	if err := ctx.Err(); err != nil {
		return ports.DispatchReceipt{}, context.Cause(ctx)
	}
	receipt, sendErr := provider.Deliver(ctx, request)
	if sendErr == nil {
		if err := mysql.ValidateDispatchReceipt(request, receipt); err != nil {
			return ports.DispatchReceipt{}, err
		}
		if receipt.Outcome == ports.DispatchAccepted || receipt.Outcome == ports.DispatchRejected {
			return receipt, nil
		}
	}
	// Lost response may hide an accepted effect. Never infer failure, create a
	// new domain identity, or mark successful from a transport error alone.
	if err := ctx.Err(); err != nil {
		return ports.DispatchReceipt{}, errors.Join(ErrDispatchUncertain, sendErr, context.Cause(ctx))
	}
	receipt, err := check(provider.Reconcile(ctx, request))
	if err != nil {
		return ports.DispatchReceipt{}, errors.Join(sendErr, err)
	}
	if receipt.Outcome != ports.DispatchAccepted && receipt.Outcome != ports.DispatchRejected {
		return ports.DispatchReceipt{}, errors.Join(ErrDispatchUncertain, sendErr)
	}
	return receipt, nil
}
