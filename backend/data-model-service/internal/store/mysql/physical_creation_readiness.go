package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type physicalCreationReadiness struct {
	shared    *installation.ExactKeyReadiness
	installer installation.RecordInstallerPolicy
}

// The publication transaction owns the model/registry/intent locks. Check the
// complete initial snapshot after owner effects, before serializing its contract.
// Missing mappings, extra fields/components or altered mandatory semantics must
// roll back all owner effects instead of publishing metadata not enforced by DDL.
func (e *PhysicalExecutor) verifyCreationMetadataTx(ctx context.Context, tx *sql.Tx, table, operation uuid.UUID) error {
	var raw sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT CASE WHEN OCTET_LENGTH(CAST(desired_shape AS CHAR CHARACTER SET utf8mb4))<=1048576 THEN CAST(desired_shape AS CHAR CHARACTER SET utf8mb4) END FROM "+e.schema+".data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&raw); err != nil {
		return err
	}
	if !raw.Valid {
		return ErrSnapshotLimit
	}
	plan, err := decodePhysicalPlan([]byte(raw.String), e.database, e.creation.installer.Table)
	if err != nil {
		return err
	}
	c := plan.Creation
	if c == nil || c.TenantID != e.tenant || c.TableID != table {
		return ErrPhysicalConflict
	}
	if plan.Version == 3 {
		return e.verifyNativeMetadataTx(ctx, tx, table, plan)
	}
	query := "SELECT f.id,CASE WHEN OCTET_LENGTH(f.name)<=64 THEN f.name END,f.data_type,f.nullable,f.is_unique,f.is_enum,f.archived,p.component,p.physical_name FROM " + e.schema + ".data_model_fields f LEFT JOIN " + e.schema + ".data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.tenant_id=? AND f.table_id=?"
	args := []any{mysqlstore.UUID(e.tenant), mysqlstore.UUID(table)}
	query += " ORDER BY f.id,p.component LIMIT 3"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var id mysqlstore.UUID
		var name, component, column sql.NullString
		var kind string
		var nullable, unique, enum, archived bool
		if err = rows.Scan(&id, &name, &kind, &nullable, &unique, &enum, &archived, &component, &column); err != nil {
			break
		}
		count++
		valid := name.Valid && component.Valid && column.Valid && component.String == "value" && !nullable && !enum && !archived
		if uuid.UUID(id) == c.ObjectFieldID {
			valid = valid && name.String == "object_id" && kind == "string" && unique && column.String == c.ObjectColumn
		} else if uuid.UUID(id) == c.UpdatedFieldID {
			valid = valid && name.String == "updated_at" && kind == "timestamp" && !unique && column.String == c.UpdatedColumn
		} else {
			valid = false
		}
		if !valid || count > 2 {
			err = ErrPhysicalConflict
			break
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if count != 2 {
		return ErrPhysicalConflict
	}
	return nil
}

// WithCreationReadiness returns an independent, explicitly configured publisher.
// Unconfigured executors continue rejecting v2 publication. Shared installation
// and privilege administration must be excluded through publication, as for v1.
// The installer must be locked, disconnected and stripped of SET_ANY_DEFINER
// before verification; this method never changes accounts or server settings.
func (e *PhysicalExecutor) WithCreationReadiness(shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy) (*PhysicalExecutor, error) {
	if e == nil || e.db == nil || shared == nil {
		return nil, errors.New("executor and shared creation dependencies required")
	}
	if _, err := installer.Account(true); err != nil {
		return nil, err
	}
	copy := *e
	copy.creation = &physicalCreationReadiness{shared: shared, installer: installer}
	return &copy, nil
}

func creationDefinerPolicy(name string, definer installation.Definer) installation.AccountPolicy {
	return installation.AccountPolicy{User: definer.User, Host: definer.Host, Locked: true, Objects: []installation.ObjectGrant{
		{Kind: "TABLE", Name: name, Privileges: []string{"SELECT", "UPDATE", "TRIGGER"}},
		{Kind: "PROCEDURE", Name: "data_model_resolve_exact_key", Privileges: []string{"EXECUTE"}},
	}}
}

func (e *PhysicalExecutor) verifyCreationReadiness(ctx context.Context, conn *sql.Conn, name string, plan PhysicalDDLPlan, policies []PhysicalAccessPolicy) error {
	cfg := e.creation
	if cfg == nil || plan.Creation == nil {
		return ErrPhysicalPlanningUnsupported
	}
	if cfg.installer.NativeChanges != (plan.Version == 3) {
		return ErrPhysicalPlanningUnsupported
	}
	if cfg.installer.Table != name || plan.Creation.TenantID != e.tenant {
		return ErrPhysicalConflict
	}
	definer := plan.Creation.Definer
	seen := map[string]bool{}
	for _, principal := range []installation.Definer{definer, cfg.installer.Principal} {
		key := principal.User + "@" + strings.ToLower(principal.Host)
		if seen[key] {
			return errors.New("distinct record definer and installer required")
		}
		seen[key] = true
	}
	for _, policy := range policies {
		key := policy.Account.User + "@" + strings.ToLower(policy.Account.Host)
		if seen[key] {
			return errors.New("distinct creation and physical consumer principals required")
		}
		seen[key] = true
		for _, object := range policy.Account.Objects {
			if object.Name == "data_model_exact_keys" || object.Name == "data_model_exact_key_buckets" {
				return errors.New("physical consumers cannot access the private dictionary")
			}
		}
	}
	if err := e.creation.shared.VerifyOnConn(ctx, conn, e.database, definer); err != nil {
		return err
	}
	if err := cfg.installer.VerifyInactive(ctx, conn, e.database); err != nil {
		return err
	}
	if err := installation.VerifyAccountPolicy(ctx, conn, e.database, creationDefinerPolicy(name, definer)); err != nil {
		return err
	}
	return installation.VerifyNoAccountSessions(ctx, conn, definer)
}
