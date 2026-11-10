package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

type Worker struct {
	DB         *sql.DB
	Database   string
	Store      *native.Store
	Dispatch   service.DispatchService
	Continuous service.ContinuousWorkerService
	Datasets   service.DatasetUpdateWorkerService
	Delivery   *service.CaseDeliveryService
	Log        *slog.Logger
}

func (w Worker) Tick(ctx context.Context) error {
	if w.Store == nil {
		return errors.New("native worker store required")
	}
	sources, e := w.Store.Screenings().ListByStatus(ctx, "pending", 25)
	if e != nil {
		return e
	}
	recoverable, e := w.Store.Screenings().ListRecoverable(ctx, time.Now().UTC(), 25)
	if e != nil {
		return e
	}
	sources = append(sources, recoverable...)
	for _, v := range sources {
		tenant, e := uuid.Parse(v.TenantID)
		if e != nil {
			return e
		}
		run, cancel := context.WithTimeout(native.WithTenant(ctx, tenant), 20*time.Second)
		e = w.Dispatch.RunScreening(run, v.TenantID, v.ID)
		cancel()
		w.report("dispatch", e)
	}
	objects, e := w.Store.MonitoredObjects().ListByStatus(ctx, "pending", 25)
	if e != nil {
		return e
	}
	for _, v := range objects {
		token, e := w.Store.ClaimWork(ctx, v.TenantID, "continuous", v.ID)
		if errors.Is(e, ports.ErrScreeningBusy) {
			continue
		}
		if e != nil {
			return e
		}
		tenant, e := uuid.Parse(v.TenantID)
		if e != nil {
			return e
		}
		run, cancel := context.WithTimeout(native.WithWork(native.WithTenant(ctx, tenant), v.TenantID, "continuous", v.ID, token), 20*time.Second)
		e = w.Continuous.RunMonitoredObject(run, v.TenantID, v.ID)
		cancel()
		w.report("continuous", e)
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var release error
		if e == nil {
			release = w.Store.ReleaseWork(cleanup, v.TenantID, "continuous", v.ID, token)
		} else {
			release = w.Store.BackoffWork(cleanup, v.TenantID, "continuous", v.ID, token)
		}
		stop()
		if release != nil && !errors.Is(release, ports.ErrScreeningBusy) {
			return release
		}
	}
	pending, e := w.Store.DatasetUpdateJobs().ListByStatus(ctx, "pending", 25)
	if e != nil {
		return e
	}
	processing, e := w.Store.DatasetUpdateJobs().ListByStatus(ctx, "processing", 25)
	if e != nil {
		return e
	}
	pending = append(pending, processing...)
	for _, v := range pending {
		token, e := w.Store.ClaimWork(ctx, v.TenantID, "dataset", v.ID)
		if errors.Is(e, ports.ErrScreeningBusy) {
			continue
		}
		if e != nil {
			return e
		}
		tenant, e := uuid.Parse(v.TenantID)
		if e != nil {
			return e
		}
		run, cancel := context.WithTimeout(native.WithWork(native.WithTenant(ctx, tenant), v.TenantID, "dataset", v.ID, token), 20*time.Second)
		if v.Status == "processing" {
			e = w.Store.Run(run, func(m ports.MutationStore) error { return m.(*native.Store).ResetDataset(run, v.TenantID, v.ID) })
		}
		if e == nil {
			e = w.Datasets.RunJob(run, v.TenantID, v.ID)
		}
		cancel()
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var release error
		if e == nil {
			release = w.Store.ReleaseWork(cleanup, v.TenantID, "dataset", v.ID, token)
		} else {
			release = w.Store.BackoffWork(cleanup, v.TenantID, "dataset", v.ID, token)
		}
		stop()
		w.report("dataset", e)
		if release != nil {
			return release
		}
	}
	if w.Delivery != nil {
		if e = w.Delivery.RunBatch(ctx, 25); e != nil {
			return e
		}
	}
	if w.DB != nil {
		schema,e:=mysqlstore.Identifier(w.Database);if e!=nil{return e}
		if _,e=w.DB.ExecContext(ctx,"DELETE FROM "+schema+".screening_local_blobs WHERE content IS NULL AND upload_until<=UTC_TIMESTAMP(6) ORDER BY upload_until,id LIMIT 100");e!=nil{return e}
		if e = ProjectBatch(ctx, w.DB, w.Database); e != nil {
			return e
		}
	}
	return nil
}
func (w Worker) report(phase string, e error) {
	if e != nil && !errors.Is(e, ports.ErrScreeningBusy) && w.Log != nil {
		w.Log.Warn("screening work requires retry", "phase", phase)
	}
}
func (w Worker) Run(ctx context.Context, ready func()) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		run, cancel := context.WithTimeout(ctx, 45*time.Second)
		e := w.Tick(run)
		cancel()
		if e == nil {
			ready()
		} else {
			w.report("cycle", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
