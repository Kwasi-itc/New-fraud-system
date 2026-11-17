package service

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/google/uuid"
	"time"
)

// Legacy provider maintenance keeps its existing global operator boundary. It
// does not infer tenant-private authority from a job's tenant or request JSON.
// Internal publications use the separate entitled subscription fanout.
func (s DatasetUpdateWorkerService) runMaintenance(ctx context.Context, item screening.DatasetUpdateJob) error {
	ctx = provideraccess.WithScope(ctx, provideraccess.GlobalOperator())
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	token := uuid.NewString()
	var cp ports.DatasetCheckpoint
	var claimed bool
	err := s.txManager.Run(bounded, func(store ports.MutationStore) error {
		var e error
		cp, claimed, e = store.DatasetUpdateJobs().(ports.DatasetMaintenanceRepository).ClaimMaintenance(bounded, item.TenantID, item.ID, token, s.clock.Now())
		return e
	})
	if err != nil || !claimed {
		return err
	}
	finish := func(status, code string) error {
		clean, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		return s.txManager.Run(clean, func(store ports.MutationStore) error {
			return store.DatasetUpdateJobs().(ports.DatasetMaintenanceRepository).FinishMaintenance(clean, item.TenantID, item.ID, token, status, code, s.clock.Now())
		})
	}
	for page := 0; !cp.DeltaDone; page++ {
		if page >= 1000 {
			return finish("failed", "dataset_page_budget_exhausted")
		}
		delta, e := s.provider.GetDatasetDelta(bounded, item.Provider, cp.Cursor)
		if e != nil {
			return finish("failed", "provider_delta_failed")
		}
		e = s.txManager.Run(bounded, func(store ports.MutationStore) error {
			var e error
			cp, e = store.DatasetUpdateJobs().(ports.DatasetMaintenanceRepository).SaveDeltaPage(bounded, item.TenantID, item.ID, token, delta, s.clock.Now())
			return e
		})
		if e != nil {
			return finish("failed", "dataset_delta_checkpoint_failed")
		}
	}
	for page := 0; !cp.ObjectsDone; page++ {
		if page >= 1000 {
			return finish("failed", "object_page_budget_exhausted")
		}
		e := s.txManager.Run(bounded, func(store ports.MutationStore) error {
			ids, done, e := store.DatasetUpdateJobs().(ports.DatasetMaintenanceRepository).RequeueMaintenancePage(bounded, item.TenantID, item.ID, token, s.clock.Now())
			if e != nil {
				return e
			}
			for _, id := range ids {
				if e = s.monitoredEnq.EnqueueTx(bounded, store.RawTx(), item.TenantID, id, nil); e != nil {
					return e
				}
			}
			cp.ObjectsDone = done
			return nil
		})
		if e != nil {
			return finish("failed", "object_page_enqueue_failed")
		}
	}
	if err = finish("completed", ""); err != nil {
		return errors.Join(errors.New("maintenance_completion_retained"), err)
	}
	return nil
}
