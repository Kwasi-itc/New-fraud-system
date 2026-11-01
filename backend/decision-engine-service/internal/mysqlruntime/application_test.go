package mysqlruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

func TestMissingRuntimeProvidersFailBeforeDialing(t *testing.T) {
	if _, err := New(Dependencies{}, Policy{}); !errors.Is(err, mysql.ErrOwnerContractUnavailable) {
		t.Fatal(err)
	}
	var nilRuntime *Runtime
	if _, err := nilRuntime.Start(context.Background(), nil); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal(err)
	}
}

func TestApplicationWorkerFailureCancelsAndJoinsSiblings(t *testing.T) {
	want := errors.New("required worker failed")
	entered := make(chan struct{})
	joined := make(chan struct{})
	a := &Application{workers: []workerLoop{
		func(ctx context.Context, _ func(context.Context, error) error) error {
			close(entered)
			<-ctx.Done()
			close(joined)
			return context.Cause(ctx)
		},
		func(context.Context, func(context.Context, error) error) error { <-entered; return want },
	}}
	a.ready.Store(true)
	if err := a.RunWorkers(context.Background(), func(context.Context, error) error { return nil }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("worker returned before sibling stopped")
	}
	if a.ready.Load() || !a.stopped.Load() {
		t.Fatal("worker failure left application ready")
	}
	if err := a.RunWorkers(context.Background(), func(context.Context, error) error { return nil }); err == nil {
		t.Fatal("restarted stopped worker set")
	}
}

func TestApplicationShutdownIsBoundedAndRejectsAdmissionFirst(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	var observed atomic.Bool
	a := &Application{workers: []workerLoop{func(ctx context.Context, _ func(context.Context, error) error) error {
		close(entered)
		<-ctx.Done()
		observed.Store(true)
		<-release
		return context.Cause(ctx)
	}}}
	a.ready.Store(true)
	go func() {
		finished <- a.RunWorkers(context.Background(), func(context.Context, error) error { return nil })
	}()
	<-entered
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if a.ready.Load() || !a.stopped.Load() || !observed.Load() {
		t.Fatal("shutdown did not withdraw readiness/cancel work")
	}
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUnexpectedSuccessfulWorkerExitIsFailure(t *testing.T) {
	a := &Application{workers: []workerLoop{func(context.Context, func(context.Context, error) error) error { return nil }}}
	if err := a.RunWorkers(context.Background(), func(context.Context, error) error { return nil }); err == nil {
		t.Fatal("required worker silently exited")
	}
}
