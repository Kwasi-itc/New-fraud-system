package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type PhysicalAccessPolicy struct {
	// Mode is reader, writer or ddl. A writer and a separate DDL principal are
	// mandatory. Writer grants exclude DDL; DDL grants exclude physical DML.
	Mode    string
	Account installation.AccountPolicy
}

func validatePhysicalPolicies(database, name string, policies []PhysicalAccessPolicy) error {
	if len(policies) < 2 || len(policies) > 32 {
		return errors.New("2..32 physical account policies required")
	}
	seen := map[string]bool{}
	modes := map[string]bool{}
	for _, policy := range policies {
		account := policy.Account
		key := account.User + "@" + account.Host
		if seen[key] || account.Locked {
			return errors.New("distinct unlocked physical service accounts required")
		}
		seen[key] = true
		if _, err := installation.GrantStatements(database, account); err != nil {
			return err
		}
		var required map[string]bool
		switch policy.Mode {
		case "reader":
			required = map[string]bool{"SELECT": true}
		case "writer":
			required = map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true}
		case "ddl":
			required = map[string]bool{"SELECT": true, "CREATE": true, "ALTER": true, "INDEX": true}
		default:
			return errors.New("unknown physical account mode")
		}
		modes[policy.Mode] = true
		found := false
		for _, object := range account.Objects {
			// EXECUTE can confer a definer's DML or DDL authority even when
			// the caller has no direct mutation/schema grants. Native v1 has
			// no reviewed routine capability contract for these principals.
			if object.Kind != "TABLE" {
				return errors.New("physical account routine capabilities require a separate reviewed policy")
			}
			privileges := append([]string(nil), object.Privileges...)
			for privilege := range object.Columns {
				privileges = append(privileges, privilege)
			}
			for _, privilege := range privileges {
				if policy.Mode != "ddl" {
					switch privilege {
					case "CREATE", "DROP", "ALTER", "INDEX", "TRIGGER", "REFERENCES", "CREATE VIEW":
						return errors.New("physical consumer account has DDL privileges")
					}
				}
				if policy.Mode == "reader" {
					switch privilege {
					case "INSERT", "UPDATE", "DELETE":
						return errors.New("physical reader account has mutation privileges")
					}
				}
			}
		}
		for _, object := range account.Objects {
			if object.Kind == "TABLE" && object.Name == name {
				// Native physical roles require exact table-wide permissions.
				// Column grants cannot add capabilities or substitute for that
				// contract (including DML hidden on the DDL principal).
				if len(object.Columns) != 0 {
					return errors.New("physical target requires exact table-wide role permissions")
				}
				found = true
				for _, privilege := range object.Privileges {
					if !required[privilege] {
						return errors.New("physical account violates mode separation")
					}
					delete(required, privilege)
				}
			}
		}
		if !found || len(required) != 0 {
			return errors.New("physical account lacks exact table permissions")
		}
	}
	if !modes["writer"] || !modes["ddl"] {
		return errors.New("writer and DDL policies required")
	}
	return nil
}

// WithVerifiedPhysical holds the table's DDL lock through final shape/grant
// inspection and the owner's callback. It never opens the registry itself.
// The callback must be synchronous local publication work on this connection;
// it must recheck authorization/version and atomically publish all owner effects.
// It must not close/retain the connection, change locks, run DDL or call networks.
// A nil callback is invalid: an unlocked verification snapshot is not proof for
// later publication. This is staged owner integration, not an API success path.
func (e *PhysicalExecutor) WithVerifiedPhysical(ctx context.Context, table, operation uuid.UUID, policies []PhysicalAccessPolicy, publish func(*sql.Conn) error) error {
	if table == uuid.Nil || operation == uuid.Nil || publish == nil {
		return errors.New("table, operation and owner callback required")
	}
	sum := sha256.Sum256([]byte(e.database + ":" + e.tenant.String() + ":" + table.String()))
	return mysqlstore.WithDDLLock(ctx, e.db, fmt.Sprintf("data_model_ddl_%x", sum[:24]), func(conn *sql.Conn) error {
		return e.withVerifiedPhysicalConn(ctx, conn, table, operation, policies, publish)
	})
}

// The caller holds the table DDL lock on conn throughout this method.
func (e *PhysicalExecutor) withVerifiedPhysicalConn(ctx context.Context, conn *sql.Conn, table, operation uuid.UUID, policies []PhysicalAccessPolicy, publish func(*sql.Conn) error) error {
	var name, gate, state string
	var epoch, expectedEpoch int64
	var active, version, expectedVersion mysqlstore.NullUUID
	if err := conn.QueryRowContext(ctx, "SELECT physical_name,write_state,epoch,active_operation,published_version FROM "+e.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table)).Scan(&name, &gate, &epoch, &active, &version); err != nil {
		return err
	}
	if gate != "closed" || !active.Valid || uuid.UUID(active.UUID) != operation {
		return ErrPhysicalConflict
	}
	var raw sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT state,expected_epoch,expected_version,CASE WHEN OCTET_LENGTH(CAST(desired_shape AS CHAR CHARACTER SET utf8mb4))<=1048576 THEN CAST(desired_shape AS CHAR CHARACTER SET utf8mb4) END FROM "+e.schema+".data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&state, &expectedEpoch, &expectedVersion, &raw); err != nil {
		return err
	}
	if state != "verifying" || epoch != expectedEpoch+1 || version.Valid != expectedVersion.Valid || (version.Valid && version.UUID != expectedVersion.UUID) {
		return ErrPhysicalConflict
	}
	if !raw.Valid {
		return ErrSnapshotLimit
	}
	plan, err := decodePhysicalPlan([]byte(raw.String), e.database, name)
	if err != nil {
		return err
	}
	// V2 execution alone cannot attest the shared resolver lineage or locked
	// trigger-definer privileges. Never publish via the native v1 grant contract.
	if plan.Version != 1 && e.creation == nil {
		return ErrPhysicalPlanningUnsupported
	}
	if plan.Version != 2 && plan.Version != 3 && e.creation != nil {
		return ErrPhysicalPlanningUnsupported
	}
	if (plan.Version == 2 && version.Valid) || (plan.Version == 3 && !version.Valid) {
		return ErrPhysicalConflict
	}
	if plan.Creation != nil && (plan.Creation.TenantID != e.tenant || plan.Creation.TableID != table) {
		return ErrPhysicalConflict
	}
	if err := validatePhysicalPolicies(e.database, name, policies); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, "SELECT step_no,checksum,state FROM "+e.schema+".data_model_schema_change_steps WHERE tenant_id=? AND operation_id=? ORDER BY step_no LIMIT 1001", mysqlstore.UUID(e.tenant), mysqlstore.UUID(operation))
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var number int
		var checksum []byte
		var progress string
		if err = rows.Scan(&number, &checksum, &progress); err != nil {
			break
		}
		count++
		if number != count || count > len(plan.Steps) || progress != "applied" {
			err = migrations.ErrLineage
			break
		}
		expected := physicalChecksum(e.database, plan.Steps[count-1])
		if !bytes.Equal(checksum, expected[:]) {
			err = migrations.ErrLineage
			break
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if count != len(plan.Steps) {
		return migrations.ErrLineage
	}
	if plan.Version == 2 {
		if err := verifyCreationProgress(ctx, conn, e.database, name, plan, []string{"applied", "applied", "applied"}); err != nil {
			return err
		}
		if err := e.verifyCreationReadiness(ctx, conn, name, plan, policies); err != nil {
			return err
		}
	} else {
		actual, err := migrations.Definition(ctx, conn, e.database, "TABLE", name)
		if err != nil {
			return err
		}
		if actual != plan.Steps[len(plan.Steps)-1].After {
			return migrations.ErrDrift
		}
		if plan.Version == 3 {
			if err := verifyRetainedRecordGuards(ctx, conn, e.database, name, plan); err != nil {
				return err
			}
			if err := e.verifyCreationReadiness(ctx, conn, name, plan, policies); err != nil {
				return err
			}
		} else {
			// Native v1 plans contain table DDL only. SHOW CREATE TABLE does not
			// describe triggers; an unplanned trigger must not pass readiness.
			var trigger string
			err = conn.QueryRowContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE=? LIMIT 1", e.database, name).Scan(&trigger)
			if err == nil {
				return migrations.ErrDrift
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	}
	for _, policy := range policies {
		if err := installation.VerifyAccountPolicy(ctx, conn, e.database, policy.Account); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return publish(conn)
}
