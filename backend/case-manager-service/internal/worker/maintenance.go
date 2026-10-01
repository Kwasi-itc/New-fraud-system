package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

const Queue = "case_maintenance"
const DeliveryQueue = "case_delivery"
const Schema = "case_queue"

type Args struct{}

func (Args) Kind() string { return "case_maintenance" }

type DeliveryArgs struct{}

func (DeliveryArgs) Kind() string { return "case_delivery" }

type DeliveryWorker struct {
	river.WorkerDefaults[DeliveryArgs]
	Worker *Worker
}

func (w *DeliveryWorker) Work(ctx context.Context, _ *river.Job[DeliveryArgs]) error {
	return w.Worker.Deliver(ctx)
}

type Worker struct {
	river.WorkerDefaults[Args]
	Store          store.Maintenance
	Limit          int
	PublisherURL   string
	PublisherToken string
}

func (w *Worker) Work(ctx context.Context, _ *river.Job[Args]) error { return w.Maintain(ctx) }
func (w *Worker) Run(ctx context.Context) error {
	if err := w.Maintain(ctx); err != nil {
		return err
	}
	return w.Deliver(ctx)
}
func (w *Worker) Maintain(ctx context.Context) error {
	if err := w.Store.CleanUploads(ctx, w.Limit); err != nil {
		return err
	}
	if _, err := w.Store.WakeSnoozed(ctx, w.Limit); err != nil {
		return err
	}
	if _, err := w.Store.AssignWaiting(ctx, w.Limit); err != nil {
		return err
	}
	return nil
}
func (w *Worker) Deliver(ctx context.Context) error {
	if w.PublisherURL == "" {
		return nil
	} // Explicitly disabled: pending events remain durable.
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for i := 0; i < w.Limit; i++ {
		d, err := w.Store.ClaimDelivery(ctx)
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		body, err := json.Marshal(d)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.PublisherURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", d.ID.String())
		if w.PublisherToken != "" {
			req.Header.Set("Authorization", "Bearer "+w.PublisherToken)
		}
		response, err := client.Do(req)
		failure := ""
		if err != nil {
			failure = "delivery transport failed"
		} else {
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				failure = fmt.Sprintf("delivery HTTP %d", response.StatusCode)
			}
			_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			closeErr := response.Body.Close()
			if failure == "" && (readErr != nil || closeErr != nil) {
				failure = "delivery response incomplete"
			}
		}
		if err := w.Store.FinishDelivery(ctx, *d, failure); err != nil {
			return err
		}
	}
	return nil
}

func ValidatePublisher(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("OUTBOX_PUBLISHER_URL must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return nil
}

func NewClient(db *pgxpool.Pool, w *Worker, interval time.Duration) (*river.Client[pgx.Tx], error) {
	if w.Limit < 1 || w.Limit > 1000 || interval < time.Second {
		return nil, fmt.Errorf("worker limit must be 1..1000 and interval at least 1s")
	}
	if err := ValidatePublisher(w.PublisherURL); err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	river.AddWorker(workers, &DeliveryWorker{Worker: w})
	unique := river.UniqueOpts{ByQueue: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}}
	return river.NewClient(riverpgxv5.New(db), &river.Config{
		Schema: Schema, Workers: workers, Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}, DeliveryQueue: {MaxWorkers: 1}},
		JobTimeout: 30 * time.Minute,
		PeriodicJobs: []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(interval), func() (river.JobArgs, *river.InsertOpts) {
			return Args{}, &river.InsertOpts{Queue: Queue, MaxAttempts: 5, UniqueOpts: unique}
		}, &river.PeriodicJobOpts{ID: Queue, RunOnStart: true}), river.NewPeriodicJob(river.PeriodicInterval(interval), func() (river.JobArgs, *river.InsertOpts) {
			if w.PublisherURL == "" {
				return nil, nil
			}
			return DeliveryArgs{}, &river.InsertOpts{Queue: DeliveryQueue, MaxAttempts: 5, UniqueOpts: unique}
		}, &river.PeriodicJobOpts{ID: DeliveryQueue, RunOnStart: true})},
	})
}

// Dedicated River schema shares the application database but isolates scheduler
// leadership from services that do not register case maintenance periodic jobs.
func Migrate(ctx context.Context, db *pgxpool.Pool) (result error) {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(196701,4)`); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(196701,4)`); err != nil {
			// Never return a connection holding a session lock to the pool.
			_ = conn.Conn().Close(cleanup)
			if result == nil {
				result = fmt.Errorf("release migration lock: %w", err)
			}
		}
	}()
	if _, err = conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS case_queue`); err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(db), &rivermigrate.Config{Schema: Schema})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}
