package mysql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func TestCancellationAttemptNeverCancelsTerminalOrInventsAttempt(t *testing.T) {
	for _, test := range []struct {
		status      execution.Status
		count, want int
	}{{execution.StatusPending, 0, 1}, {execution.StatusPending, 1, 2}, {execution.StatusRunning, 1, 1}, {execution.StatusRunning, 3, 3}} {
		got, err := cancellationAttempt(test.status, execution.StatusPending, test.count, 3)
		if err != nil || got != test.want {
			t.Fatal(test, got, err)
		}
	}
	for _, test := range []struct {
		status execution.Status
		count  int
	}{{execution.StatusRunning, 0}, {execution.StatusPending, 3}, {execution.StatusCompleted, 1}, {execution.StatusFailed, 1}, {ExecutionStatusCancelled, 1}, {execution.StatusPending, -1}} {
		if _, err := cancellationAttempt(test.status, execution.StatusPending, test.count, 3); err == nil {
			t.Fatal(test)
		}
	}
	if !terminalExecutionStatus(ExecutionStatusCancelled) || terminalExecutionStatus(execution.StatusRunning) {
		t.Fatal("wrong terminal callback contract")
	}
}

func TestDeliveryRetentionUsesSharedPolicyAndRejectsLiveCleanup(t *testing.T) {
	for _, test := range []struct {
		state     workers.DeliveryState
		retention time.Duration
	}{{workers.Completed, 24 * time.Hour}, {workers.Cancelled, 24 * time.Hour}, {workers.Discarded, 7 * 24 * time.Hour}} {
		got, err := deliveryRetention(test.state)
		if err != nil || got != test.retention {
			t.Fatal(test, got, err)
		}
	}
	for _, state := range []workers.DeliveryState{workers.Available, workers.Running, "unknown"} {
		if _, err := deliveryRetention(state); err == nil {
			t.Fatal("live/unknown state cleaned", state)
		}
	}
	s, err := NewEffectDeliveryStore(&sql.DB{}, "fraud", uuid.New(), ports.JobOutbox, "outbox", time.Minute, func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1, 101} {
		if _, err := s.CleanupTerminal(context.Background(), workers.Completed, limit); err == nil {
			t.Fatal("unbounded cleanup accepted")
		}
	}
	if _, _, err := s.CleanupTerminalPage(context.Background(), workers.Completed, &DeliveryCleanupCursor{}, 1); err == nil {
		t.Fatal("bad cleanup cursor accepted")
	}
}
