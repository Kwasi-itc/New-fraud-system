package installation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

// ExactKeyReadiness binds shared dictionary/resolver dependencies to a reviewed
// complete installation artifact. It is immutable and does not cache successful
// inspections. It neither grants privileges nor opens physical readiness.
type ExactKeyReadiness struct {
	database       string
	definer        Definer
	runner         *migrations.Runner
	receiptDefiner *Definer
	resultDefiner  *Definer
}

// NewExactKeyReadiness accepts only the shipped v001 lineage and optional v002
// delivery upgrade. Unknown upgrades require explicit source binding here before
// they can authorize creation dependencies. Reference definitions must come from
// a reviewed isolated artifact, never from observing the application target.
func NewExactKeyReadiness(db *sql.DB, plan migrations.Plan, definer Definer) (*ExactKeyReadiness, error) {
	return newExactKeyReadiness(db, plan, definer, nil, nil)
}

// NewExactKeyReadinessWithCreationReceipts binds the complete v003 lineage and
// its separate locked receipt definer. Older constructors reject this upgrade.
func NewExactKeyReadinessWithCreationReceipts(db *sql.DB, plan migrations.Plan, definer, receiptDefiner Definer) (*ExactKeyReadiness, error) {
	if definer.User == receiptDefiner.User && strings.EqualFold(definer.Host, receiptDefiner.Host) {
		return nil, errors.New("distinct shared and receipt definers required")
	}
	return newExactKeyReadiness(db, plan, definer, &receiptDefiner, nil)
}

func newExactKeyReadiness(db *sql.DB, plan migrations.Plan, definer Definer, receiptDefiner, resultDefiner *Definer) (*ExactKeyReadiness, error) {
	base, err := Statements(plan.Database, definer)
	if err != nil {
		return nil, err
	}
	extra := 0
	if resultDefiner != nil {
		extra = 4
		if receiptDefiner == nil {
			return nil, errors.New("owner results require creation receipts")
		}
	}
	if (receiptDefiner == nil && len(plan.Steps) != len(base) && len(plan.Steps) != len(base)+1) || (receiptDefiner != nil && len(plan.Steps) != len(base)+5+extra) {
		return nil, errors.New("complete supported installation lineage required")
	}
	bound, err := Plan(plan.Database, definer, plan.Steps[:len(base)])
	if err != nil {
		return nil, err
	}
	if len(plan.Steps) > len(base) {
		bound, err = ExtendDeliveryRetryPlan(bound, plan.Steps[len(base)])
		if err != nil {
			return nil, err
		}
	}
	if receiptDefiner != nil {
		bound, err = ExtendCreationReceiptPlan(bound, *receiptDefiner, plan.Steps[len(base)+1:len(base)+5])
		if err != nil {
			return nil, err
		}
	}
	if resultDefiner != nil {
		bound, err = ExtendOwnerResultPlan(bound, *resultDefiner, plan.Steps[len(base)+5:])
		if err != nil {
			return nil, err
		}
	}
	runner, err := migrations.New(db, bound)
	if err != nil {
		return nil, err
	}
	return &ExactKeyReadiness{database: plan.Database, definer: definer, runner: runner, receiptDefiner: receiptDefiner, resultDefiner: resultDefiner}, nil
}

// VerifyOnConn requires the caller's physical execution lock and operational
// exclusion of shared-schema and privilege administration through subsequent
// publication. It uses the existing connection, even with a one-slot pool.
// Complete applied history, all final installation shapes, the dedicated locked
// resolver definer's exact grants and absence of dictionary triggers are checked
// every time. No DDL, repair, account change or readiness mutation is performed.
// Trigger-definer, restricted installer and owner checks remain separate gates.
func (r *ExactKeyReadiness) VerifyOnConn(ctx context.Context, conn *sql.Conn, database string, triggerDefiner Definer) error {
	if r == nil || r.runner == nil || conn == nil || database != r.database {
		return errors.New("same-database shared dependency verifier and pinned connection required")
	}
	if _, err := triggerDefiner.SQLAccount(); err != nil {
		return err
	}
	if triggerDefiner.User == r.definer.User && strings.EqualFold(triggerDefiner.Host, r.definer.Host) {
		return errors.New("record trigger definer must be distinct from shared resolver definer")
	}
	return r.runner.VerifyOnConn(ctx, conn, func(conn *sql.Conn) error {
		if r.resultDefiner != nil {
			if err := r.verifyOwnerResults(ctx, conn, triggerDefiner); err != nil {
				return err
			}
		}
		if r.receiptDefiner != nil {
			if triggerDefiner.User == r.receiptDefiner.User && strings.EqualFold(triggerDefiner.Host, r.receiptDefiner.Host) {
				return errors.New("record and receipt definers must be distinct")
			}
			if err := VerifyAccountPolicy(ctx, conn, r.database, CreationReceiptDefinerPolicy(*r.receiptDefiner)); err != nil {
				return err
			}
			if err := VerifyNoAccountSessions(ctx, conn, *r.receiptDefiner); err != nil {
				return err
			}
			rows, err := conn.QueryContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE='data_model_table_creation_operations' ORDER BY TRIGGER_NAME LIMIT 4", r.database)
			if err != nil {
				return err
			}
			want := map[string]bool{"data_model_table_creation_operations_insert_guard": true, "data_model_table_creation_operations_update_guard": true, "data_model_table_creation_operations_delete_guard": true}
			for rows.Next() {
				var name string
				if err = rows.Scan(&name); err != nil {
					break
				}
				if !want[name] {
					err = migrations.ErrDrift
					break
				}
				delete(want, name)
			}
			err = errors.Join(err, rows.Err(), rows.Close())
			if err != nil {
				return err
			}
			if len(want) != 0 {
				return migrations.ErrDrift
			}
		}
		if err := VerifyDefiner(ctx, conn, r.database, r.definer); err != nil {
			return fmt.Errorf("shared resolver definer: %w", err)
		}
		// Locking an account does not terminate previously authenticated sessions.
		// A definer login could otherwise mutate the dictionary directly. Inspect
		// all hosts conservatively, as in the locked-account grant installer.
		var enabled, sessions bool
		if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.performance_schema").Scan(&enabled); err != nil {
			return err
		}
		if !enabled {
			return errors.New("performance_schema required to inspect definer sessions")
		}
		if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM performance_schema.threads WHERE TYPE='FOREGROUND' AND PROCESSLIST_USER=?)", r.definer.User).Scan(&sessions); err != nil {
			return err
		}
		if sessions {
			return errors.New("shared resolver definer has existing sessions")
		}
		// SHOW CREATE TABLE excludes triggers. Neither dictionary table has any
		// reviewed triggers; even one additional trigger changes allocation.
		var name string
		err := conn.QueryRowContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE IN ('data_model_exact_keys','data_model_exact_key_buckets') LIMIT 1", r.database).Scan(&name)
		if err == nil {
			return fmt.Errorf("unexpected exact-key dictionary trigger: %w", migrations.ErrDrift)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return ctx.Err()
	})
}
