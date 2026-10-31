package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
)

// ValidateQueueReadiness uses the SAME role/pool as the API or worker. It never
// installs/repairs queue objects or accepts a connectivity ping as readiness.
// This is a version/object/access observation, not a full schema drift audit.
// Subsequent DDL can invalidate it; mutations must still fail and roll back.
func ValidateQueueReadiness(ctx context.Context, db *pgxpool.Pool, worker bool) (result error) {
	if db == nil {
		return errors.New("queue database is required")
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(db), &rivermigrate.Config{Schema: riverjobs.Schema})
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			result = errors.Join(result, err)
		}
	}()
	history, err := migrator.ExistingVersionsTx(ctx, tx)
	if err != nil {
		return err
	}
	want := migrator.AllVersions()
	if len(history) != len(want) {
		return errors.New("River schema version is missing or incompatible")
	}
	versions := make(map[int]bool, len(want))
	for _, version := range want {
		versions[version.Version] = true
	}
	for _, version := range history {
		if !versions[version.Version] {
			return errors.New("River schema lineage is incompatible")
		}
	}
	rows, err := tx.Query(ctx, `SELECT id,state,attempt,max_attempts,args,scheduled_at,created_at,attempted_at,attempted_by,finalized_at,errors,metadata,kind,priority,queue,tags,unique_key,unique_states FROM public.river_job LIMIT 0`)
	if err != nil {
		return err
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, table := range []string{"public.river_job", "public.river_migration", "public.river_queue", "public.river_leader", "public.river_client", "public.river_client_queue"} {
		var present bool
		if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&present); err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("required River object %s is missing", table)
		}
		privileges := []string{}
		if table == "public.river_job" {
			privileges = []string{"SELECT", "INSERT", "UPDATE"}
			if worker {
				privileges = append(privileges, "DELETE")
			}
		} else if table == "public.river_migration" {
			privileges = []string{"SELECT"}
		} else if worker {
			privileges = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
		}
		for _, privilege := range privileges {
			var allowed bool
			if err := tx.QueryRow(ctx, "SELECT has_table_privilege(current_user,$1,$2)", table, privilege).Scan(&allowed); err != nil {
				return err
			}
			if !allowed {
				return fmt.Errorf("required River %s access is missing for %s", privilege, table)
			}
		}
	}
	var compatible bool
	if err := tx.QueryRow(ctx, `SELECT has_sequence_privilege(current_user,'public.river_job_id_seq','USAGE')
 AND has_type_privilege(current_user,'public.river_job_state','USAGE')
 AND has_function_privilege(current_user,'public.river_job_state_in_bitmask(bit,public.river_job_state)','EXECUTE')
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_index WHERE indexrelid=to_regclass('public.river_job_unique_idx') AND indisunique AND indisvalid AND indisready)`).Scan(&compatible); err != nil {
		return err
	}
	if !compatible {
		return errors.New("River sequence/type/function/index compatibility or access is missing")
	}
	return tx.Commit(ctx)
}
