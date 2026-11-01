package mysqlexecution

import (
	"context"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

// SchedulerPolicy bounds each tick to PageSize owner transactions with at most
// OccurrencesPerSchedule occurrences each. Catch-up work is deliberately paced;
// one late schedule cannot monopolize an unbounded loop or transaction.
type SchedulerPolicy struct {
	PageSize, OccurrencesPerSchedule int
	Interval, TickTimeout            time.Duration
}

type Scheduler struct {
	owner  *mysql.ExecutionStore
	policy SchedulerPolicy
}

func NewScheduler(owner *mysql.ExecutionStore, policy SchedulerPolicy) (*Scheduler, error) {
	_, rows, _, err := owner.ExecutionPreparationScope()
	if err != nil {
		return nil, err
	}
	if policy.PageSize < 1 || policy.PageSize > min(rows, 100) || policy.OccurrencesPerSchedule < 1 || policy.OccurrencesPerSchedule > min(rows, 100) || policy.Interval < 10*time.Millisecond || policy.Interval > time.Hour || policy.TickTimeout < time.Millisecond || policy.TickTimeout > time.Minute {
		return nil, errors.New("bounded scheduler page/occurrences, interval [10ms,1h] and tick timeout [1ms,1m] required")
	}
	return &Scheduler{owner, policy}, nil
}

// Tick has no retained transaction or network callback. The caller retains the
// returned cursor only after success. Each materialization rechecks current
// authorization, definition/live binding and permanent occurrence receipts.
// A commit with uncertain outcome returns an error: never rerun this tick as an
// automatic transaction retry. A new process can reconcile durable watermarks.
func (s *Scheduler) Tick(ctx context.Context, after *mysql.ScheduleCursor, report func(context.Context, error) error) (*mysql.ScheduleCursor, error) {
	if s == nil || s.owner == nil || report == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	var page []mysql.ScheduleCursor
	err := s.owner.Run(ctx, mysql.ExecutionScheduleTick, func(r mysql.ExecutionRepositories) error {
		var err error
		page, err = r.Schedules.Due(ctx, after, s.policy.PageSize)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, entry := range page {
		err := s.owner.Run(ctx, mysql.ExecutionScheduleTick, func(r mysql.ExecutionRepositories) error {
			_, err := r.Schedules.Materialize(ctx, entry.ID, s.policy.OccurrencesPerSchedule)
			return err
		})
		if err == nil {
			continue
		}
		// Concurrent deactivation/republication is an observable skipped schedule,
		// not successful materialization. All other errors stop this pass.
		if errors.Is(err, mysql.ErrExecutionState) && !ambiguousTransaction(err) {
			if err := report(ctx, err); err != nil {
				return nil, err
			}
			continue
		}
		return nil, err
	}
	if len(page) < s.policy.PageSize {
		return nil, nil
	}
	last := page[len(page)-1]
	return &last, nil
}

// Run uses a process-local keyset cursor, wraps it only after a complete sweep,
// and sleeps between bounded pages even when catch-up remains. Multiple service
// replicas are safe through owner row locking plus occurrence receipts, not a
// global scheduler lock. No worker is registered by constructing this component.
func (s *Scheduler) Run(ctx context.Context, report func(context.Context, error) error) error {
	if s == nil || s.owner == nil || report == nil {
		return mysql.ErrDecisionAuthorizationUnavailable
	}
	var cursor *mysql.ScheduleCursor
	for {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		tick, cancel := context.WithTimeout(ctx, s.policy.TickTimeout)
		next, err := s.Tick(tick, cursor, report)
		cancel()
		if err != nil {
			return err
		}
		cursor = next
		timer := time.NewTimer(s.policy.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
}
