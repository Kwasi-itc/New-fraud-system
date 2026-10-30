package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// ValidateQueueReadiness checks pinned lineage, required objects and access
// using the runtime role/pool. It neither migrates nor repairs shared objects.
// This is not a complete schema drift audit: later DDL can invalidate readiness,
// and required queue INSERT failures must still roll back the owner transaction.
func ValidateQueueReadiness(ctx context.Context, db *pgxpool.Pool, worker bool) (result error) {
	if db == nil {
		return errors.New("decision queue database is required")
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
			result = errors.Join(result, fmt.Errorf("rollback queue readiness: %w", err))
		}
	}()
	history, err := migrator.ExistingVersionsTx(ctx, tx)
	if err != nil {
		return err
	}
	want := migrator.AllVersions()
	versions := make(map[int]bool, len(want))
	for _, version := range want {
		versions[version.Version] = true
	}
	if len(history) != len(versions) {
		return errors.New("River schema version is missing or incompatible")
	}
	for _, version := range history {
		if !versions[version.Version] {
			return errors.New("River schema lineage is incompatible")
		}
		delete(versions, version.Version)
	}
	// Resolve the actual job projection without reading any job payloads.
	rows, err := tx.Query(ctx, `SELECT id,state,attempt,max_attempts,args,scheduled_at,created_at,attempted_at,attempted_by,finalized_at,errors,metadata,kind,priority,queue,tags,unique_key,unique_states FROM public.river_job LIMIT 0`)
	if err != nil {
		return err
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var allowed bool
	// Bounded catalog work, independent of queue size. API producers do not
	// require maintenance DELETE access; workers do. Identifiers are constants.
	err = tx.QueryRow(ctx, `WITH required(name,access) AS (VALUES
 ('public.river_job',CASE WHEN $1 THEN 'SELECT,INSERT,UPDATE,DELETE' ELSE 'SELECT,INSERT,UPDATE' END),
 ('public.river_migration','SELECT'),
 ('public.river_queue',CASE WHEN $1 THEN 'SELECT,INSERT,UPDATE,DELETE' ELSE NULL END),
 ('public.river_leader',CASE WHEN $1 THEN 'SELECT,INSERT,UPDATE,DELETE' ELSE NULL END),
 ('public.river_client',CASE WHEN $1 THEN 'SELECT,INSERT,UPDATE,DELETE' ELSE NULL END),
 ('public.river_client_queue',CASE WHEN $1 THEN 'SELECT,INSERT,UPDATE,DELETE' ELSE NULL END)
 ) SELECT bool_and(to_regclass(name) IS NOT NULL AND
   (access IS NULL OR (SELECT bool_and(has_table_privilege(current_user,to_regclass(name),privilege))
     FROM unnest(string_to_array(access,',')) privilege)))
 FROM required`, worker).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("required River tables or runtime table access are missing")
	}
	err = tx.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'public','USAGE')
 AND has_sequence_privilege(current_user,'public.river_job_id_seq','USAGE')
 AND has_type_privilege(current_user,'public.river_job_state','USAGE')
 AND has_function_privilege(current_user,'public.river_job_state_in_bitmask(bit,public.river_job_state)','EXECUTE')
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_index WHERE indexrelid=to_regclass('public.river_job_unique_idx') AND indisunique AND indisvalid AND indisready)`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("River schema/sequence/type/function/index compatibility or access is missing")
	}
	return tx.Commit(ctx)
}
