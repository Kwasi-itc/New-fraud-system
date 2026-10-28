package reconcile

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type recoveryQueueFailure struct {
	riverjobs.IndexJobEnqueuer
	cancel context.CancelFunc
}

func (e recoveryQueueFailure) EnqueueTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, at *time.Time) error {
	if err := e.IndexJobEnqueuer.EnqueueTx(ctx, tx, id, at); err != nil {
		return err
	}
	if e.cancel != nil {
		e.cancel()
		return nil
	}
	return errRepairInjected
}

func TestLegacyDeliveryRecovery(t *testing.T) {
	pool, _, tenant, table := repairFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	recovery, err := NewDeliveryRecovery(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := storepostgres.NewIndexJobRepository(pool)
	seed := func() datamodel.IndexJob {
		t.Helper()
		at := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
		id := uuid.New()
		job := datamodel.IndexJob{ID: id, TenantID: tenant.ID, TableID: &table.ID, TableName: table.Name, IndexType: datamodel.IndexJobTypeRepair, Columns: []string{"object_id"}, Status: datamodel.IndexJobStatusPending, RequestedByOperation: "reconcile_repair", RequestedAt: at.Add(-time.Hour), ScheduledAt: &at, AttemptCount: 2, DedupeKey: id.String()}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		return job
	}
	request := func(job datamodel.IndexJob) DeliveryRecoveryRequest {
		return DeliveryRecoveryRequest{TenantID: tenant.ID, JobID: job.ID, Apply: true, Actor: "test-operator", Reason: "recovery test"}
	}
	counts := func(job datamodel.IndexJob) (int, int) {
		t.Helper()
		var queued, audited int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job WHERE kind='index_job_execute' AND args->>'index_job_id'=$1`, job.ID.String()).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM core.schema_change_log WHERE operation='recover_index_job_delivery' AND resource_id=$1`, job.ID).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		return queued, audited
	}
	t.Run("inspect_recover_replay", func(t *testing.T) {
		job := seed()
		before, err := repo.GetByID(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		r := request(job)
		r.Apply = false
		result, err := recovery.Run(ctx, r)
		if err != nil || !result.Eligible || result.Recovered {
			t.Fatalf("inspect: %+v %v", result, err)
		}
		if q, a := counts(job); q != 0 || a != 0 {
			t.Fatalf("inspect wrote q=%d a=%d", q, a)
		}
		r.Apply = true
		result, err = recovery.Run(ctx, r)
		if err != nil || !result.Recovered {
			t.Fatalf("recover: %+v %v", result, err)
		}
		var scheduled time.Time
		if err := pool.QueryRow(ctx, `SELECT scheduled_at FROM public.river_job WHERE args->>'index_job_id'=$1`, job.ID.String()).Scan(&scheduled); err != nil || !scheduled.Equal(*job.ScheduledAt) {
			t.Fatalf("schedule: %v %v", scheduled, err)
		}
		after, err := repo.GetByID(ctx, job.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("changed owner job: %+v %v", after, err)
		}
		result, err = recovery.Run(ctx, r)
		if err != nil || result.Recovered || result.Eligible || !result.DeliveryExists {
			t.Fatalf("replay: %+v %v", result, err)
		}
		if q, a := counts(job); q != 1 || a != 1 {
			t.Fatalf("replay q=%d a=%d", q, a)
		}
		var attributed bool
		if err := pool.QueryRow(ctx, `SELECT details->>'database_principal'=session_user AND details->>'actor'=$2 AND details->>'reason'=$3 FROM core.schema_change_log WHERE resource_id=$1 AND operation='recover_index_job_delivery'`, job.ID, r.Actor, r.Reason).Scan(&attributed); err != nil || !attributed {
			t.Fatalf("audit attribution: %v %v", attributed, err)
		}
	})
	t.Run("configured_queue", func(t *testing.T) {
		job := seed()
		client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
		if err != nil {
			t.Fatal(err)
		}
		configured, err := NewDeliveryRecovery(pool, riverjobs.NewRiverIndexJobEnqueuer(client, 7, "custom_index_queue"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := configured.Run(ctx, request(job))
		if err != nil || !r.Recovered {
			t.Fatalf("configured producer: %+v %v", r, err)
		}
		var queue string
		var attempts int
		if err := pool.QueryRow(ctx, `SELECT queue,max_attempts FROM public.river_job WHERE args->>'index_job_id'=$1`, job.ID.String()).Scan(&queue, &attempts); err != nil || queue != "custom_index_queue" || attempts != 7 {
			t.Fatalf("queue=%s attempts=%d err=%v", queue, attempts, err)
		}
	})
	t.Run("concurrent_recovery", func(t *testing.T) {
		job := seed()
		type outcome struct {
			r   DeliveryRecoveryResult
			err error
		}
		done := make(chan outcome, 2)
		for range 2 {
			go func() { r, err := recovery.Run(ctx, request(job)); done <- outcome{r, err} }()
		}
		wins := 0
		for range 2 {
			o := <-done
			if o.err != nil {
				t.Fatal(o.err)
			}
			if o.r.Recovered {
				wins++
			}
		}
		if q, a := counts(job); wins != 1 || q != 1 || a != 1 {
			t.Fatalf("wins=%d q=%d a=%d", wins, q, a)
		}
	})
	for _, state := range []string{"available", "pending", "running", "retryable", "scheduled", "completed", "cancelled", "discarded"} {
		t.Run("existing_"+state, func(t *testing.T) {
			job := seed()
			// Legacy transport rows may have no River uniqueness metadata. Do not
			// rely on InsertTx deduplication to detect them. Any queue blocks repair.
			_, err := pool.Exec(ctx, `INSERT INTO public.river_job(kind,args,queue,state,max_attempts,finalized_at) VALUES ('index_job_execute',jsonb_build_object('index_job_id',$1::text,'legacy',true),'legacy_queue',$2::text::public.river_job_state,10,CASE WHEN $2::text IN ('completed','cancelled','discarded') THEN now() ELSE NULL END)`, job.ID.String(), state)
			if err != nil {
				t.Fatal(err)
			}
			r, err := recovery.Run(ctx, request(job))
			if err != nil || r.Eligible || r.Recovered || !r.DeliveryExists {
				t.Fatalf("existing: %+v %v", r, err)
			}
			if q, a := counts(job); q != 1 || a != 0 {
				t.Fatalf("q=%d a=%d", q, a)
			}
		})
	}
	t.Run("wrong_tenant_and_invalid_requests", func(t *testing.T) {
		job := seed()
		r := request(job)
		r.TenantID = uuid.New()
		if _, err := recovery.Run(ctx, r); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("wrong tenant: %v", err)
		}
		for _, r := range []DeliveryRecoveryRequest{{}, {TenantID: tenant.ID, JobID: job.ID, Apply: true}, {TenantID: tenant.ID, JobID: job.ID, Apply: true, Actor: "operator"}} {
			if _, err := recovery.Run(ctx, r); err == nil {
				t.Fatal("invalid request accepted")
			}
		}
		if q, a := counts(job); q != 0 || a != 0 {
			t.Fatalf("q=%d a=%d", q, a)
		}
	})
	for _, status := range []datamodel.IndexJobStatus{datamodel.IndexJobStatusRunning, datamodel.IndexJobStatusApplied, datamodel.IndexJobStatusFailed, datamodel.IndexJobStatusCancelled} {
		t.Run("owner_"+string(status), func(t *testing.T) {
			job := seed()
			if _, err := pool.Exec(ctx, `UPDATE core.index_jobs SET status=$2 WHERE id=$1`, job.ID, status); err != nil {
				t.Fatal(err)
			}
			r, err := recovery.Run(ctx, request(job))
			if err != nil || r.Eligible || r.Recovered || r.Reason != "job_not_pending" {
				t.Fatalf("state: %+v %v", r, err)
			}
			if q, a := counts(job); q != 0 || a != 0 {
				t.Fatalf("q=%d a=%d", q, a)
			}
		})
	}
	for _, change := range []string{"operation", "type", "missing_table", "wrong_table"} {
		t.Run(change, func(t *testing.T) {
			job := seed()
			query := map[string]string{"operation": `UPDATE core.index_jobs SET requested_by_operation='manual' WHERE id=$1`, "type": `UPDATE core.index_jobs SET index_type='search' WHERE id=$1`, "missing_table": `UPDATE core.index_jobs SET table_id=NULL WHERE id=$1`, "wrong_table": `UPDATE core.index_jobs SET table_name='other' WHERE id=$1`}[change]
			if _, err := pool.Exec(ctx, query, job.ID); err != nil {
				t.Fatal(err)
			}
			r, err := recovery.Run(ctx, request(job))
			if err != nil || r.Eligible || r.Recovered {
				t.Fatalf("identity: %+v %v", r, err)
			}
			if q, a := counts(job); q != 0 || a != 0 {
				t.Fatalf("q=%d a=%d", q, a)
			}
		})
	}
	t.Run("history_failure_rolls_back_delivery", func(t *testing.T) {
		job := seed()
		_, err := pool.Exec(ctx, `CREATE FUNCTION core.reject_recovery_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='recover_index_job_delivery' THEN RAISE EXCEPTION 'injected history failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_recovery_history BEFORE INSERT ON core.schema_change_log FOR EACH ROW EXECUTE FUNCTION core.reject_recovery_history()`)
		if err != nil {
			t.Fatal(err)
		}
		r, err := recovery.Run(ctx, request(job))
		if err == nil || r.Recovered {
			t.Fatalf("history failure accepted: %+v %v", r, err)
		}
		if q, a := counts(job); q != 0 || a != 0 {
			t.Fatalf("q=%d a=%d", q, a)
		}
		if _, err := pool.Exec(ctx, `DROP TRIGGER reject_recovery_history ON core.schema_change_log; DROP FUNCTION core.reject_recovery_history()`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("queue_failure_rolls_back_delivery", func(t *testing.T) {
		job := seed()
		// Use the fixture's producer, but fail after its successful INSERT.
		client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
		if err != nil {
			t.Fatal(err)
		}
		producer := riverjobs.NewRiverIndexJobEnqueuer(client, 10, riverjobs.IndexJobQueueName)
		faulty, err := NewDeliveryRecovery(pool, recoveryQueueFailure{IndexJobEnqueuer: producer})
		if err != nil {
			t.Fatal(err)
		}
		r, err := faulty.Run(ctx, request(job))
		if !errors.Is(err, errRepairInjected) || r.Recovered {
			t.Fatalf("queue failure: %+v %v", r, err)
		}
		if q, a := counts(job); q != 0 || a != 0 {
			t.Fatalf("q=%d a=%d", q, a)
		}
	})
	t.Run("cancellation_rolls_back_delivery", func(t *testing.T) {
		job := seed()
		client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
		if err != nil {
			t.Fatal(err)
		}
		requestCtx, stop := context.WithCancel(ctx)
		defer stop()
		producer := riverjobs.NewRiverIndexJobEnqueuer(client, 10, riverjobs.IndexJobQueueName)
		faulty, err := NewDeliveryRecovery(pool, recoveryQueueFailure{IndexJobEnqueuer: producer, cancel: stop})
		if err != nil {
			t.Fatal(err)
		}
		r, err := faulty.Run(requestCtx, request(job))
		if err == nil || r.Recovered {
			t.Fatalf("cancellation: %+v %v", r, err)
		}
		if q, a := counts(job); q != 0 || a != 0 {
			t.Fatalf("q=%d a=%d", q, a)
		}
	})
}
