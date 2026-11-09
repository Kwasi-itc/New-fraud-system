package installation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// WorkerIndexUpgradeStatements appends operational lookup indexes without
// rewriting frozen v001 metadata. No table data or prior checksums are changed.
func WorkerIndexUpgradeStatements(database string) ([]migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	return []migrations.Step{
		{ID: "data_model_v007_jobs_table_scope", Kind: "TABLE", Name: "data_model_index_jobs", SQL: "ALTER TABLE " + schema + ".data_model_index_jobs ADD INDEX data_model_jobs_table_scope (tenant_id,table_id,requested_at DESC,id DESC)"},
		{ID: "data_model_v007_intent_epoch", Kind: "TABLE", Name: "data_model_schema_change_intents", SQL: "ALTER TABLE " + schema + ".data_model_schema_change_intents ADD INDEX data_model_intent_epoch (tenant_id,table_id,expected_epoch,state)"},
	}, nil
}

func ExtendWorkerIndexPlan(installed migrations.Plan, reference []migrations.Step) (migrations.Plan, error) {
	if len(installed.Steps) == 0 || installed.Steps[len(installed.Steps)-1].ID != "data_model_v006_contract_lookup" {
		return migrations.Plan{}, errors.New("worker indexes require v006 lineage")
	}
	want, err := WorkerIndexUpgradeStatements(installed.Database)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(reference) != len(want) {
		return migrations.Plan{}, errors.New("complete worker index references required")
	}
	for i, got := range reference {
		if got.ID != want[i].ID || got.Kind != want[i].Kind || got.Name != want[i].Name || got.SQL != want[i].SQL || got.Before == "" {
			return migrations.Plan{}, errors.New("worker index reference differs from source")
		}
	}
	bound, err := ExtendPlan(installed, reference)
	bound.Owner = installed.Owner
	return bound, err
}

// CaptureWorkerIndexUpgrade is explicitly invoked by the local installer on
// its separate disposable reference server. Reconstruct these two reference
// tables from the retained source before capturing: ALTER can normalize CHECK
// literals and retire implicit FK indexes, so DROP INDEX is not its inverse.
// The installer verifies separate server UUIDs before invoking this callback.
func CaptureWorkerIndexUpgrade(ctx context.Context, reference *sql.DB, installed migrations.Plan) (migrations.Plan, error) {
	steps, err := WorkerIndexUpgradeStatements(installed.Database)
	if err != nil {
		return migrations.Plan{}, err
	}
	conn, err := reference.Conn(ctx)
	if err != nil {
		return migrations.Plan{}, err
	}
	defer conn.Close()
	schema, _ := mysqlstore.Identifier(installed.Database)
	for i := range steps {
		step := &steps[i]
		for _, old := range installed.Steps {
			if old.Kind == step.Kind && old.Name == step.Name {
				step.Before = old.After
			}
		}
		if err := resetWorkerReferenceTable(ctx, conn, schema, step.Name, installed); err != nil {
			return migrations.Plan{}, err
		}
		actual, err := migrations.Definition(ctx, conn, installed.Database, step.Kind, step.Name)
		if err != nil {
			return migrations.Plan{}, err
		}
		if actual != step.Before {
			return migrations.Plan{}, migrations.ErrDrift
		}
		if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
			return migrations.Plan{}, err
		}
		if step.Name == "data_model_index_jobs" {
			// SHOW CREATE renders the original implicit FK key as explicit when
			// reconstructed. Emulate InnoDB's retirement of that implicit key
			// after the wider covering key is added on the original target.
			var count int
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=? AND table_name=? AND index_name='tenant_id_2'", installed.Database, step.Name).Scan(&count); err != nil {
				return migrations.Plan{}, err
			}
			if count > 0 {
				if _, err := conn.ExecContext(ctx, "ALTER TABLE "+schema+".data_model_index_jobs DROP INDEX tenant_id_2"); err != nil {
					return migrations.Plan{}, err
				}
			}
		}
		step.After, err = migrations.Definition(ctx, conn, installed.Database, step.Kind, step.Name)
		if err != nil {
			return migrations.Plan{}, err
		}
	}
	return ExtendWorkerIndexPlan(installed, steps)
}

func resetWorkerReferenceTable(ctx context.Context, conn *sql.Conn, schema, name string, source migrations.Plan) (result error) {
	if name != "data_model_index_jobs" && name != "data_model_schema_change_intents" {
		return errors.New("worker reference table required")
	}
	var statements []string
	// Replay original retained SQL, not SHOW CREATE: the latter loses implicit
	// FK-index and inherited-character-set metadata when parsed as new DDL.
	for _, step := range source.Steps {
		if step.Kind == "TABLE" && step.Name == name {
			statements = append(statements, step.SQL)
		}
	}
	if len(statements) == 0 || !strings.HasPrefix(statements[0], "CREATE TABLE "+schema+"."+name+" ") {
		return errors.New("original retained worker table SQL required")
	}
	for _, step := range source.Steps {
		if step.Kind == "TRIGGER" && strings.HasPrefix(step.Name, name+"_") {
			statements = append(statements, step.SQL)
		}
	}
	// Dedicated reference connection only. Always restore/discard the session;
	// application pools and target tables never enter this reconstruction path.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := conn.ExecContext(cleanup, "SET SESSION foreign_key_checks=1")
		if err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		result = errors.Join(result, err)
	}()
	statements = append([]string{"SET SESSION foreign_key_checks=0", "DROP TABLE IF EXISTS " + schema + ".`" + name + "`"}, statements...)
	for _, q := range statements {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
