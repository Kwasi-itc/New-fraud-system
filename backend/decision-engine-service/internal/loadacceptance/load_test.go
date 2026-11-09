package loadacceptance

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func smokeProfile() Profile {
	return Profile{Duration: 200 * time.Millisecond, Rate: 50, Workers: 2, Timeout: time.Second, P95: time.Second, P99: time.Second}
}
func TestRunRetainsEveryOfferedRequestAndFailure(t *testing.T) {
	p := smokeProfile()
	var calls atomic.Int32
	report, err := Run(context.Background(), p, func(_ context.Context, i int) error {
		calls.Add(1)
		if i == 3 {
			return errors.New("injected")
		}
		return nil
	})
	if err == nil || report.MeetsTargets || report.Failed != 1 || report.Succeeded != 9 || calls.Load() != 10 || len(report.Samples) != 10 {
		t.Fatalf("failure or offered work lost: %+v %v", report, err)
	}
	for i, s := range report.Samples {
		if s.Index != i || s.Scheduled != time.Duration(i)*20*time.Millisecond {
			t.Fatal("schedule drift", s)
		}
	}
}
func TestSaturationCannotPassByOmittingWork(t *testing.T) {
	p := smokeProfile()
	p.Rate = 1000
	p.Workers = 1
	p.Timeout = 100 * time.Millisecond
	report, err := Run(context.Background(), p, func(ctx context.Context, _ int) error { <-ctx.Done(); return ctx.Err() })
	if err == nil || report.MeetsTargets || report.Dropped == 0 || report.Failed == 0 || report.Succeeded+report.Failed+report.Dropped+report.Canceled != report.Offered {
		t.Fatalf("saturation concealed: %+v %v", report, err)
	}
}
func TestCancellationJoinsWorkersAndKeepsDenominator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := Run(ctx, smokeProfile(), func(context.Context, int) error { t.Error("canceled work executed"); return nil })
	if !errors.Is(err, context.Canceled) || report.Canceled != report.Offered || report.MeetsTargets {
		t.Fatal(report, err)
	}
}
func TestProfileRejectsUnboundedOrNonfiniteTargets(t *testing.T) {
	for _, mutate := range []func(*Profile){func(p *Profile) { p.MaxErrorRate = math.NaN() }, func(p *Profile) { p.Workers = 0 }, func(p *Profile) { p.Rate = 0 }, func(p *Profile) { p.Duration = time.Hour; p.Rate = 10000 }, func(p *Profile) { p.P99 = p.P95 - 1 }} {
		p := smokeProfile()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid profile accepted", p)
		}
	}
}
