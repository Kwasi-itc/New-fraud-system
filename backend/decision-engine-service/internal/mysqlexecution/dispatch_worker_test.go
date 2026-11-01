package mysqlexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

type receiptProvider struct {
	deliver, query func(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error)
}

func (p receiptProvider) Deliver(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return p.deliver(ctx, r)
}
func (p receiptProvider) Reconcile(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return p.query(ctx, r)
}

func TestDispatchReconciliationNeverBlindlyRepeats(t *testing.T) {
	request := ports.DispatchRequest{Key: "stable", PayloadSHA256: "digest"}
	accepted := ports.DispatchReceipt{Key: request.Key, PayloadSHA256: request.PayloadSHA256, Outcome: ports.DispatchAccepted, Reference: "provider-1"}
	for _, tc := range []struct {
		name    string
		resumed bool
		outcome ports.DispatchOutcome
		sendErr bool
		sends   int
		success bool
	}{
		{"fresh", false, ports.DispatchAccepted, false, 1, true},
		{"lost response reconciled", false, ports.DispatchAccepted, true, 1, true},
		{"crashed after send", true, ports.DispatchAccepted, false, 0, true},
		{"unknown remains blocked", true, ports.DispatchUnknown, false, 0, false},
		{"confirmed absent same key", true, ports.DispatchNotFound, false, 1, true},
		{"fresh uncertain", false, ports.DispatchUnknown, true, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sends := 0
			p := receiptProvider{deliver: func(_ context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
				sends++
				if r.Key != request.Key {
					t.Fatal("changed identity")
				}
				if tc.sendErr {
					return ports.DispatchReceipt{}, errors.New("lost response")
				}
				return accepted, nil
			}, query: func(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error) {
				v := accepted
				v.Outcome = tc.outcome
				if v.Outcome == ports.DispatchUnknown || v.Outcome == ports.DispatchNotFound {
					v.Reference = ""
				}
				return v, nil
			}}
			_, err := resolveDispatch(context.Background(), p, mysql.DispatchIntent{Request: request, Resumed: tc.resumed})
			if (err == nil) != tc.success || sends != tc.sends {
				t.Fatal(sends, err)
			}
		})
	}
	p := receiptProvider{query: func(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error) {
		v := accepted
		v.Key = "another-tenant"
		return v, nil
	}}
	if _, err := resolveDispatch(context.Background(), p, mysql.DispatchIntent{Request: request, Resumed: true}); !errors.Is(err, mysql.ErrDispatchReceipt) {
		t.Fatal("foreign receipt accepted", err)
	}
}
