package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// Active uses the tenant-scoped active-definition unique key. Absence is an
// explicit nil, not an arbitrary latest-row search across inactive history.
func (r ExecutionScheduleRepository) Active(ctx context.Context, parent uuid.UUID) (*ExecutionScheduleRecord, error) {
	if parent == uuid.Nil {
		return nil, ErrTenantScope
	}
	v, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND active_scenario_id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(parent)), r.scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ReplaceSchedule requires the precise old active identity/watermark, or an
// explicit nil identity/version zero to create in its absence. Definition IDs
// are supplied before the call for reconciliation after an uncertain commit.
// Lock order agrees with Materialize: schedule -> live link -> model registry.
// Provider IO and model/AST preparation MUST finish before this transaction.
func (s *ExecutionStore) ReplaceSchedule(ctx context.Context, reader TenantDataReader, snapshot ExecutionPreparationSnapshot, model ports.TenantModel, input ExecutionScheduleInput, expected uuid.UUID, version int64) (out ExecutionScheduleRecord, err error) {
	if s == nil || !reader.MatchesExecutionPreparationScope(s) || input.ScenarioID.String() != snapshot.parent.ID || input.ScenarioIterationID.String() != snapshot.iteration.ID || !input.Active || input.ID == expected || version < 0 || expected == uuid.Nil && version != 0 {
		return out, ErrExecutionState
	}
	guard, err := reader.admissionGuard(ctx, &snapshot, model, snapshot.parent.TriggerObjectType)
	if err != nil {
		return out, err
	}
	err = s.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
		c := reader.configuration
		if err := c.authorize(ctx, r.Schedules.tx, c.tenant, false); err != nil {
			return err
		}
		if expected != uuid.Nil {
			old, err := r.Schedules.lock(ctx, expected)
			if err != nil {
				return err
			}
			if old.ScenarioID != input.ScenarioID || !old.Active || old.WatermarkVersion != version {
				return ErrExecutionState
			}
		} else {
			old, err := r.Schedules.Active(ctx, input.ScenarioID)
			if err != nil {
				return err
			}
			if old != nil {
				return ErrExecutionState
			}
		}
		if err := r.Scheduled.lockAdmissionBinding(ctx, snapshot); err != nil {
			return err
		}
		if err := guard(ctx, r.Schedules.tx); err != nil {
			return err
		}
		if expected != uuid.Nil {
			if err := r.Schedules.SetActive(ctx, expected, version, false); err != nil {
				return err
			}
		}
		var err error
		out, err = r.Schedules.Create(ctx, input)
		return err
	})
	if err != nil {
		var duplicate *driver.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return ExecutionScheduleRecord{}, errors.Join(ErrExecutionState, err)
		}
		return ExecutionScheduleRecord{}, err
	}
	return out, nil
}

func (s *ExecutionStore) DisableSchedule(ctx context.Context, parent, id uuid.UUID, version int64) (out ExecutionScheduleRecord, err error) {
	if parent == uuid.Nil || id == uuid.Nil || version < 0 {
		return out, ErrExecutionState
	}
	err = s.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
		v, err := r.Schedules.lock(ctx, id)
		if err != nil {
			return err
		}
		if v.ScenarioID != parent || !v.Active || v.WatermarkVersion != version {
			return ErrExecutionState
		}
		if err := r.Schedules.SetActive(ctx, id, version, false); err != nil {
			return err
		}
		out, err = r.Schedules.Get(ctx, id)
		return err
	})
	if err != nil {
		return ExecutionScheduleRecord{}, err
	}
	return out, nil
}
