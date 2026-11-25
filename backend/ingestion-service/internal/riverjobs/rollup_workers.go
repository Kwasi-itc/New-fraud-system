package riverjobs

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const (
	RollupApplyJobKind     = "rollup_apply_deltas"
	RollupReconcileJobKind = "rollup_reconcile"
	RollupRetentionJobKind = "rollup_prune_retention"
)

type RollupApplyArgs struct {
	TenantID     string `json:"tenant_id"`
	DefinitionID string `json:"definition_id"`
}

func (RollupApplyArgs) Kind() string { return RollupApplyJobKind }

type RollupApplyWorker struct {
	river.WorkerDefaults[RollupApplyArgs]
	Pool *pgxpool.Pool
}

func (w RollupApplyWorker) Work(ctx context.Context, job *river.Job[RollupApplyArgs]) error {
	if w.Pool == nil {
		return nil
	}
	started := time.Now()
	_, err := storepostgres.ApplyRollupDeltaBatch(
		ctx,
		w.Pool,
		job.Args.TenantID,
		job.Args.DefinitionID,
		storepostgres.DefaultRollupApplierBatchSize,
		time.Now().UTC(),
	)
	storepostgres.RecordRollupApplierLag(time.Since(started))
	return err
}

type RollupRetentionArgs struct{}

func (RollupRetentionArgs) Kind() string { return RollupRetentionJobKind }

type RollupRetentionWorker struct {
	river.WorkerDefaults[RollupRetentionArgs]
	Pool *pgxpool.Pool
}

func (w RollupRetentionWorker) Work(ctx context.Context, _ *river.Job[RollupRetentionArgs]) error {
	if w.Pool == nil {
		return nil
	}
	_, err := storepostgres.PruneExpiredRollupBuckets(ctx, w.Pool, time.Now().UTC())
	return err
}

// RunReadyRollupApplierPass drains one batch for every 'ready' rollup definition.
func RunReadyRollupApplierPass(ctx context.Context, pool *pgxpool.Pool) error {
	repo := storepostgres.NewRollupRepository(pool)
	defs, err := repo.ListAllReadyDefinitions(ctx)
	if err != nil {
		return err
	}
	for _, def := range defs {
		if def.Status != ingestion.RollupStatusReady {
			continue
		}
		if _, err := storepostgres.ApplyRollupDeltaBatch(ctx, pool, def.TenantID, def.ID, storepostgres.DefaultRollupApplierBatchSize, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}
