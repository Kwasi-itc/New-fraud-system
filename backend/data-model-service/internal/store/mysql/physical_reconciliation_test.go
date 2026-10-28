package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPhysicalRecoveryContinuesIndependentWork(t *testing.T) {
	items := []physicalRecoveryItem{{uuid.New(), uuid.New()}, {uuid.New(), uuid.New()}, {uuid.New(), uuid.New()}}
	failure := errors.New("private SQL diagnostic")
	calls := 0
	page, err := recoverPhysicalItems(context.Background(), PhysicalRecoveryPage{Exhausted: true}, items, time.Second, func(ctx context.Context, table, operation uuid.UUID) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded operation")
		}
		if table != items[calls].table || operation != items[calls].operation {
			t.Fatal("routing changed")
		}
		calls++
		switch calls {
		case 1:
			return mysqlstore.ErrDDLLockBusy
		case 2:
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) || !errors.Is(err, mysqlstore.ErrDDLLockBusy) || calls != 3 || !page.Exhausted || page.After != items[2].operation {
		t.Fatalf("partial recovery hidden: %+v %v", page, err)
	}
	for i, want := range []string{"lock_busy", "execution_failed", "awaiting_publication"} {
		if page.Results[i].Outcome != want {
			t.Fatalf("result %d: %+v", i, page.Results[i])
		}
	}
}

func TestPhysicalRecoveryCancellationPreservesCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := []physicalRecoveryItem{{uuid.New(), uuid.New()}, {uuid.New(), uuid.New()}}
	calls := 0
	page, err := recoverPhysicalItems(ctx, PhysicalRecoveryPage{Exhausted: true}, items, time.Second, func(context.Context, uuid.UUID, uuid.UUID) error {
		calls++
		cancel()
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || page.Exhausted || page.After != items[0].operation || len(page.Results) != 1 {
		t.Fatalf("cancelled page: %+v %v", page, err)
	}
}

func TestPhysicalRecoveryTimeoutAllowsNextOperation(t *testing.T) {
	items := []physicalRecoveryItem{{uuid.New(), uuid.New()}, {uuid.New(), uuid.New()}}
	calls := 0
	page, err := recoverPhysicalItems(context.Background(), PhysicalRecoveryPage{Exhausted: true}, items, time.Millisecond, func(ctx context.Context, _, _ uuid.UUID) error {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 2 || page.Results[0].Outcome != "deadline_exceeded" || page.Results[1].Outcome != "awaiting_publication" {
		t.Fatalf("deadline isolation: %+v %v", page, err)
	}
}

func TestPhysicalRecoveryErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{migrations.ErrDrift, "schema_drift"}, {migrations.ErrLineage, "journal_conflict"},
		{ErrPhysicalConflict, "operation_conflict"}, {ErrPhysicalPrecondition, "data_precondition"},
		{errors.Join(migrations.ErrDrift, &mysqlstore.CommitError{Cause: errors.New("lost ack")}), "outcome_uncertain"},
		{errors.Join(context.Canceled, &mysqlstore.DDLLockCleanupError{Cause: errors.New("lost socket")}), "outcome_uncertain"},
	} {
		if got := physicalRecoveryCode(tc.err); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}

func TestPhysicalRecoveryRejectsInvalidBounds(t *testing.T) {
	e := &PhysicalExecutor{}
	for _, tc := range []struct {
		state   string
		limit   int
		timeout time.Duration
	}{
		{"published", 1, time.Second}, {"prepared", 0, time.Second}, {"prepared", 1001, time.Second}, {"blocked", 1, 0},
	} {
		if _, err := e.ReconcilePage(context.Background(), tc.state, uuid.Nil, tc.limit, tc.timeout); err == nil {
			t.Fatal("invalid recovery request accepted")
		}
	}
}
