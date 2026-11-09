// Package loadacceptance measures an explicit, bounded offered workload.
// It supplies no default acceptance target and does not approve a profile.
package loadacceptance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Profile struct {
	Duration     time.Duration `json:"duration_ns"`
	Rate         int           `json:"requests_per_second"`
	Workers      int           `json:"workers"`
	Timeout      time.Duration `json:"request_timeout_ns"`
	P95          time.Duration `json:"p95_target_ns"`
	P99          time.Duration `json:"p99_target_ns"`
	MaxErrorRate float64       `json:"max_error_rate"`
}

func (p Profile) Validate() error {
	if p.Duration < time.Millisecond || p.Duration > time.Hour || p.Rate < 1 || p.Rate > 10000 || p.Workers < 1 || p.Workers > 64 || p.Timeout < time.Millisecond || p.Timeout > time.Minute || p.P95 <= 0 || p.P99 < p.P95 || !(p.MaxErrorRate >= 0 && p.MaxErrorRate < 1) {
		return errors.New("explicit bounded duration/rate/workers/timeout/latency/error targets required")
	}
	if p.Count() < 1 || p.Count() > 1000000 {
		return errors.New("offered workload must contain 1..1000000 requests")
	}
	return nil
}
func (p Profile) Count() int { return int(int64(p.Duration) * int64(p.Rate) / int64(time.Second)) }

type Sample struct {
	Index      int           `json:"index"`
	Scheduled  time.Duration `json:"scheduled_ns"`
	QueueDelay time.Duration `json:"queue_delay_ns"`
	Latency    time.Duration `json:"latency_ns"`
	Outcome    string        `json:"outcome"`
}
type Report struct {
	Profile      Profile       `json:"profile"`
	Offered      int           `json:"offered"`
	Succeeded    int           `json:"succeeded"`
	Failed       int           `json:"failed"`
	Dropped      int           `json:"dropped"`
	Canceled     int           `json:"canceled"`
	P95          time.Duration `json:"p95_ns"`
	P99          time.Duration `json:"p99_ns"`
	Elapsed      time.Duration `json:"elapsed_ns"`
	MeetsTargets bool          `json:"meets_targets"`
	Samples      []Sample      `json:"samples"`
}

// Run schedules by offered rate, not completion rate. A full bounded queue is
// recorded as a dropped request; it never silently reduces the offered rate.
// Work must obey its context; all workers are joined before returning. Latency
// starts at the scheduled instant, including scheduling and queue delay.
func Run(ctx context.Context, p Profile, work func(context.Context, int) error) (Report, error) {
	report := Report{Profile: p}
	if err := p.Validate(); err != nil {
		return report, err
	}
	if work == nil {
		return report, errors.New("work callback required")
	}
	report.Offered = p.Count()
	report.Samples = make([]Sample, report.Offered)
	start := time.Now()
	for i := range report.Samples {
		report.Samples[i] = Sample{Index: i, Scheduled: time.Duration(int64(i) * int64(time.Second) / int64(p.Rate)), Outcome: "canceled"}
	}
	jobs := make(chan int, p.Workers)
	var workers sync.WaitGroup
	for range p.Workers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				sample := &report.Samples[index]
				due := start.Add(sample.Scheduled)
				sample.QueueDelay = time.Since(due)
				deadline := due.Add(p.Timeout)
				task, cancel := context.WithDeadline(ctx, deadline)
				var err error
				if task.Err() != nil {
					err = task.Err()
				} else {
					err = work(task, index)
					if err == nil {
						err = task.Err()
					}
				}
				cancel()
				sample.Latency = time.Since(due)
				sample.Outcome = "success"
				if err != nil {
					sample.Outcome = "error"
				}
			}
		}()
	}
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
schedule:
	for i := range report.Samples {
		delay := time.Until(start.Add(report.Samples[i].Scheduled))
		if delay > 0 {
			timer.Reset(delay)
			select {
			case <-ctx.Done():
				break schedule
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- i:
		default:
			report.Samples[i].Outcome = "dropped"
			report.Samples[i].QueueDelay = time.Since(start.Add(report.Samples[i].Scheduled))
		}
	}
	timer.Stop()
	close(jobs)
	workers.Wait()
	report.Elapsed = time.Since(start)
	durations := make([]time.Duration, 0, report.Offered)
	for _, s := range report.Samples {
		switch s.Outcome {
		case "success":
			report.Succeeded++
			durations = append(durations, s.Latency)
		case "error":
			report.Failed++
			durations = append(durations, s.Latency)
		case "dropped":
			report.Dropped++
		case "canceled":
			report.Canceled++
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	if len(durations) > 0 {
		report.P95 = durations[(len(durations)*95+99)/100-1]
		report.P99 = durations[(len(durations)*99+99)/100-1]
	}
	errorsCount := report.Failed + report.Dropped + report.Canceled
	report.MeetsTargets = ctx.Err() == nil && len(durations) > 0 && float64(errorsCount)/float64(report.Offered) <= p.MaxErrorRate && report.P95 <= p.P95 && report.P99 <= p.P99
	if ctx.Err() != nil {
		return report, context.Cause(ctx)
	}
	if !report.MeetsTargets {
		return report, fmt.Errorf("load targets unmet: offered=%d success=%d failed=%d dropped=%d canceled=%d p95=%s p99=%s", report.Offered, report.Succeeded, report.Failed, report.Dropped, report.Canceled, report.P95, report.P99)
	}
	return report, nil
}
