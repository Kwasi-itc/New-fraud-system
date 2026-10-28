package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PhysicalDDLPlan is trusted internal planner output stored in an immutable
// intent. SQL/definitions must never be accepted from API clients. V1 changes
// only the registered table; v2 creates that table and its two typed exact-key
// triggers; v3 applies source-bound native changes while retaining both guards.
// Definitions include complete SHOW CREATE results; unsupported
// mutable definitions require a separate contract.
type PhysicalDDLPlan struct {
	Version          int                       `json:"version"`
	Steps            []migrations.Step         `json:"steps"`
	EmptyTableBefore []string                  `json:"empty_table_before,omitempty"`
	Creation         *PhysicalCreationIdentity `json:"creation,omitempty"`
	RetainedGuards   []migrations.Step         `json:"retained_guards,omitempty"`
	Native           *PhysicalNativeSource     `json:"native,omitempty"`
}

var ErrPhysicalPrecondition = errors.New("physical change requires an empty table or an explicit backfill protocol")

type PhysicalExecutor struct {
	db               *sql.DB
	database, schema string
	tenant           uuid.UUID
	creation         *physicalCreationReadiness
}

func NewPhysicalExecutor(db *sql.DB, database string, tenant uuid.UUID) (*PhysicalExecutor, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil {
		return nil, errors.New("DDL pool and tenant required")
	}
	return &PhysicalExecutor{db: db, database: database, schema: schema, tenant: tenant}, nil
}

func decodePhysicalPlan(data []byte, database, name string) (PhysicalDDLPlan, error) {
	var plan PhysicalDDLPlan
	if len(data) == 0 || len(data) > 1<<20 {
		return plan, ErrSnapshotLimit
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return plan, errors.New("trailing physical plan data")
	}
	if (plan.Version != 1 && plan.Version != 2 && plan.Version != 3) || len(plan.Steps) == 0 || len(plan.Steps) > 1000 {
		return plan, errors.New("physical plan version and 1..1000 steps required")
	}
	if plan.Version == 2 {
		if len(plan.RetainedGuards) != 0 || plan.Native != nil {
			return plan, errors.New("creation cannot retain preexisting guards")
		}
		if err := validatePhysicalCreation(plan, database, name); err != nil {
			return plan, err
		}
		return plan, (migrations.Plan{Database: database, Steps: plan.Steps}).Validate()
	}
	if plan.Version == 3 {
		if err := validateRetainedRecordGuards(plan, database, name); err != nil {
			return plan, err
		}
	} else if plan.Creation != nil || len(plan.RetainedGuards) != 0 || plan.Native != nil {
		return plan, errors.New("creation identity requires version 2 or 3")
	}
	schema, _ := mysqlstore.Identifier(database)
	object, err := mysqlstore.Identifier(name)
	if err != nil {
		return plan, err
	}
	for _, step := range plan.Steps {
		if plan.Version == 3 && (step.Before == "" || !strings.HasPrefix(step.SQL, "ALTER TABLE ")) {
			return plan, errors.New("guarded native changes require existing table ALTER steps")
		}
		if step.Kind != "TABLE" || step.Name != name {
			return plan, errors.New("physical step outside registered table")
		}
		if !strings.HasPrefix(step.SQL, "CREATE TABLE "+schema+"."+object+" (") && !strings.HasPrefix(step.SQL, "ALTER TABLE "+schema+"."+object+" ") {
			return plan, errors.New("qualified owner-generated physical statement required")
		}
	}
	if len(plan.EmptyTableBefore) > len(plan.Steps) {
		return plan, errors.New("invalid physical preconditions")
	}
	preconditions := map[string]bool{}
	for _, id := range plan.EmptyTableBefore {
		found := false
		for _, step := range plan.Steps {
			if step.ID == id && strings.HasPrefix(step.SQL, "ALTER TABLE ") {
				found = true
			}
		}
		if !found || preconditions[id] {
			return plan, errors.New("invalid physical precondition step")
		}
		preconditions[id] = true
	}
	err = (migrations.Plan{Database: database, Steps: plan.Steps}).Validate()
	return plan, err
}

func physicalChecksum(database string, step migrations.Step) [32]byte {
	data, _ := json.Marshal(struct {
		Database string
		Step     migrations.Step
	}{database, step})
	return sha256.Sum256(data)
}

// ExecuteSteps advances only to VERIFYING, never PUBLISHED or ready. Grants,
// operation-specific validation and atomic owner publication remain mandatory.
// The dedicated pool's connection holds the same named lock for all DDL and
// inspection; no row lock spans a DDL statement. Unknown outcomes stay dirty.
func (e *PhysicalExecutor) ExecuteSteps(ctx context.Context, table, operation uuid.UUID) error {
	if table == uuid.Nil || operation == uuid.Nil {
		return errors.New("table and operation required")
	}
	lockHash := sha256.Sum256([]byte(e.database + ":" + e.tenant.String() + ":" + table.String()))
	lockName := fmt.Sprintf("data_model_ddl_%x", lockHash[:24])
	return mysqlstore.WithDDLLock(ctx, e.db, lockName, func(conn *sql.Conn) error {
		var name string
		if err := conn.QueryRowContext(ctx, "SELECT physical_name FROM "+e.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table)).Scan(&name); err != nil {
			return err
		}
		var raw sql.NullString
		var expectedEpoch int64
		if err := conn.QueryRowContext(ctx, "SELECT CASE WHEN OCTET_LENGTH(CAST(desired_shape AS CHAR CHARACTER SET utf8mb4))<=1048576 THEN CAST(desired_shape AS CHAR CHARACTER SET utf8mb4) END,expected_epoch FROM "+e.schema+".data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&raw, &expectedEpoch); err != nil {
			return err
		}
		if !raw.Valid {
			return ErrSnapshotLimit
		}
		plan, err := decodePhysicalPlan([]byte(raw.String), e.database, name)
		if err != nil {
			return err
		}
		if plan.Creation != nil && (plan.Creation.TenantID != e.tenant || plan.Creation.TableID != table) {
			return ErrPhysicalConflict
		}
		// Every journal mutation rechecks the authoritative barrier and operation in
		// registry -> intent order. Commit errors cannot be reported as success.
		mutate := func(fn func(*sql.Tx) error) (result error) {
			tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				return err
			}
			defer func() {
				if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
					result = errors.Join(result, &mysqlstore.RollbackError{Cause: err})
				}
			}()
			var epoch int64
			var gate, state string
			var active mysqlstore.NullUUID
			if err = tx.QueryRowContext(ctx, "SELECT epoch,write_state,active_operation FROM "+e.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table)).Scan(&epoch, &gate, &active); err != nil {
				return err
			}
			if gate != "closed" || epoch != expectedEpoch+1 || !active.Valid || uuid.UUID(active.UUID) != operation {
				return ErrPhysicalConflict
			}
			if err = tx.QueryRowContext(ctx, "SELECT state FROM "+e.schema+".data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=? FOR UPDATE", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&state); err != nil {
				return err
			}
			if state == "published" {
				return ErrPhysicalConflict
			}
			if err = fn(tx); err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = tx.Commit(); err != nil {
				return &mysqlstore.CommitError{Cause: err}
			}
			return nil
		}
		if err := mutate(func(*sql.Tx) error { return nil }); err != nil {
			return err
		}
		rows, err := conn.QueryContext(ctx, "SELECT step_no,checksum,state FROM "+e.schema+".data_model_schema_change_steps WHERE tenant_id=? AND operation_id=? ORDER BY step_no LIMIT 1001", mysqlstore.UUID(e.tenant), mysqlstore.UUID(operation))
		if err != nil {
			return err
		}
		var states []string
		for rows.Next() {
			var number int
			var hash []byte
			var state string
			if err = rows.Scan(&number, &hash, &state); err != nil {
				break
			}
			if number != len(states)+1 || number > len(plan.Steps) || (len(states) > 0 && states[len(states)-1] != "applied") {
				err = migrations.ErrLineage
				break
			}
			sum := physicalChecksum(e.database, plan.Steps[number-1])
			if !bytes.Equal(hash, sum[:]) || (state != "dirty" && state != "applied") {
				err = migrations.ErrLineage
				break
			}
			states = append(states, state)
		}
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		start := len(states)
		if start > 0 && states[start-1] == "dirty" {
			start--
		}
		blocked := func(cause error) error {
			code := "physical_schema_drift"
			if errors.Is(cause, ErrPhysicalPrecondition) {
				code = "physical_data_precondition"
			}
			saveErr := mutate(func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "UPDATE "+e.schema+".data_model_schema_change_intents SET state='blocked',last_error_code=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND operation_id=?", code, mysqlstore.UUID(e.tenant), mysqlstore.UUID(operation))
				return err
			})
			return errors.Join(cause, saveErr)
		}
		if plan.Version == 2 {
			if err := verifyCreationProgress(ctx, conn, e.database, name, plan, states); err != nil {
				if errors.Is(err, migrations.ErrDrift) {
					return blocked(err)
				}
				return err
			}
		} else if start == len(plan.Steps) {
			actual, err := migrations.Definition(ctx, conn, e.database, "TABLE", name)
			if err != nil {
				return err
			}
			if actual != plan.Steps[start-1].After {
				return blocked(migrations.ErrDrift)
			}
		}
		if plan.Version == 3 {
			if err := verifyRetainedRecordGuards(ctx, conn, e.database, name, plan); err != nil {
				if errors.Is(err, migrations.ErrDrift) {
					return blocked(err)
				}
				return err
			}
		}
		for i := start; i < len(plan.Steps); i++ {
			step := plan.Steps[i]
			actual, err := migrations.Definition(ctx, conn, e.database, step.Kind, step.Name)
			if err != nil {
				return err
			}
			dirty := i < len(states)
			if (!dirty && actual != step.Before) || (dirty && actual != step.Before && actual != step.After) {
				return blocked(migrations.ErrDrift)
			}
			if actual != step.After {
				for _, id := range plan.EmptyTableBefore {
					if id != step.ID {
						continue
					}
					object, _ := mysqlstore.Identifier(name)
					var present int
					err := conn.QueryRowContext(ctx, "SELECT 1 FROM "+e.schema+"."+object+" LIMIT 1").Scan(&present)
					if err == nil {
						return blocked(ErrPhysicalPrecondition)
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
				}
			}
			if !dirty {
				sum := physicalChecksum(e.database, step)
				if err := mutate(func(tx *sql.Tx) error {
					if _, err := tx.ExecContext(ctx, "INSERT INTO "+e.schema+".data_model_schema_change_steps(tenant_id,table_id,operation_id,step_no,checksum,state,started_at) VALUES (?,?,?,?,?,'dirty',UTC_TIMESTAMP(6))", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation), i+1, sum[:]); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, "UPDATE "+e.schema+".data_model_schema_change_intents SET state='applying',last_error_code='',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(operation))
					return err
				}); err != nil {
					return err
				}
			}
			if actual != step.After {
				if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
					return err
				}
				actual, err = migrations.Definition(ctx, conn, e.database, step.Kind, step.Name)
				if err != nil {
					return err
				}
				if actual != step.After {
					return blocked(migrations.ErrDrift)
				}
			}
			if err := mutate(func(tx *sql.Tx) error {
				result, err := tx.ExecContext(ctx, "UPDATE "+e.schema+".data_model_schema_change_steps SET state='applied',applied_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND operation_id=? AND step_no=? AND state='dirty'", mysqlstore.UUID(e.tenant), mysqlstore.UUID(operation), i+1)
				if err != nil {
					return err
				}
				count, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if count != 1 {
					return migrations.ErrLineage
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if plan.Version == 2 {
			if err := verifyCreationProgress(ctx, conn, e.database, name, plan, []string{"applied", "applied", "applied"}); err != nil {
				if errors.Is(err, migrations.ErrDrift) {
					return blocked(err)
				}
				return err
			}
		}
		if plan.Version == 3 {
			if err := verifyRetainedRecordGuards(ctx, conn, e.database, name, plan); err != nil {
				if errors.Is(err, migrations.ErrDrift) {
					return blocked(err)
				}
				return err
			}
		}
		return mutate(func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE "+e.schema+".data_model_schema_change_intents SET state='verifying',last_error_code='',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(operation))
			return err
		})
	})
}
