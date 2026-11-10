package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
	"strings"
)

// Check verifies supplied lineage, required table shapes, lookup capabilities,
// current publication and direct grants. It never installs or repairs anything.
// Definer/routine-body review remains the installer's independent responsibility.
func (s *Service) Check(ctx context.Context, worker bool) error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	var version, login, role string
	if err := s.DB.QueryRowContext(ctx, "SELECT VERSION(),CURRENT_USER(),CURRENT_ROLE()").Scan(&version, &login, &role); err != nil {
		return err
	}
	if !ec2test.VersionSupported(version) || role != "NONE" {
		return errors.New("pinned server and explicit direct grants required")
	}
	latest := map[string]string{}
	for _, plan := range []migrations.Plan{s.Config.DataModelPlan, s.Config.IngestionPlan} {
		runner, err := migrations.New(s.DB, plan)
		if err != nil {
			return err
		}
		history, err := runner.Status(ctx)
		if err != nil {
			return err
		}
		if len(history) != len(plan.Steps) {
			return migrations.ErrLineage
		}
		for _, h := range history {
			if h.State != "applied" {
				return migrations.ErrLineage
			}
		}
		for _, step := range plan.Steps {
			if step.Kind == "TABLE" {
				latest[step.Name] = step.After
			}
		}
	}
	user, host, ok := strings.Cut(login, "@")
	if !ok || user != s.Config.User {
		return errors.New("runtime account identity mismatch")
	}
	grantee := "'" + user + "'@'" + host + "'"
	privileges := map[string]bool{}
	for _, query := range []string{"SELECT TABLE_NAME,PRIVILEGE_TYPE,'' FROM information_schema.TABLE_PRIVILEGES WHERE GRANTEE=? AND TABLE_SCHEMA=?", "SELECT TABLE_NAME,PRIVILEGE_TYPE,COLUMN_NAME FROM information_schema.COLUMN_PRIVILEGES WHERE GRANTEE=? AND TABLE_SCHEMA=?"} {
		rows, err := s.DB.QueryContext(ctx, query, grantee, s.Config.Database)
		if err != nil {
			return err
		}
		for rows.Next() {
			var table, priv, column string
			if err := rows.Scan(&table, &priv, &column); err != nil {
				rows.Close()
				return err
			}
			privileges[table+":"+priv+":"+column] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	required := map[string][]string{}
	for _, name := range []string{"data_model_tenants", "data_model_tables", "data_model_fields", "data_model_ingestion_contracts", "data_model_physical_tables", "data_model_physical_fields"} {
		required[name] = []string{"SELECT"}
	}
	for _, name := range []string{"ingestion_receipt_buckets", "ingestion_execution_buckets", "ingestion_lookup_guards"} {
		required[name] = []string{"INSERT", "SELECT:key_digest", "UPDATE:key_digest"}
	}
	for _, name := range []string{"ingestion_request_receipts", "ingestion_execution_receipts", "ingestion_work_resources", "ingestion_work_jobs", "ingestion_work_reservations"} {
		required[name] = []string{"SELECT", "INSERT"}
	}
	required["ingestion_audit"] = []string{"INSERT"}
	required["ingestion_outbox_events"] = []string{"INSERT"}
	if worker {
		for _, col := range []string{"result_payload", "total_rows", "successful_rows", "failed_rows", "completed_at"} {
			required["ingestion_work_resources"] = append(required["ingestion_work_resources"], "UPDATE:"+col)
		}
		for _, col := range []string{"state", "available_at", "attempt", "started_at", "lease_token", "lease_until", "finished_at", "last_error"} {
			required["ingestion_work_jobs"] = append(required["ingestion_work_jobs"], "UPDATE:"+col)
		}
		required["ingestion_work_reservations"] = append(required["ingestion_work_reservations"], "DELETE")
	}
	contract, err := store.NewIngestionContractStore(s.DB, s.Config.Database, s.Config.ContractBytes)
	if err != nil {
		return err
	}
	authorized := store.WithServiceCredential(ctx, s.Config.Token)
	var physical string
	err = contract.WithRecordRead(authorized, s.Config.TenantID, s.Config.ObjectType, s.Config.Token, []string{"object_id"}, func(_ context.Context, _ *sql.Tx, p *store.RecordProjection) error {
		_, table := p.SQL()
		_, name, _ := strings.Cut(table, ".")
		physical = strings.Trim(name, "`")
		return nil
	})
	if err != nil {
		return err
	}
	required[physical] = []string{"SELECT", "INSERT", "UPDATE"}
	for table, permissions := range required {
		for _, permission := range permissions {
			priv, column, _ := strings.Cut(permission, ":")
			if !privileges[table+":"+priv+":"] && !privileges[table+":"+priv+":"+column] {
				return fmt.Errorf("required runtime grant missing: %s %s", table, permission)
			}
		}
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for table := range required {
		if expected, ok := latest[table]; ok {
			actual, err := migrations.Definition(ctx, conn, s.Config.Database, "TABLE", table)
			if err != nil {
				return err
			}
			if actual != expected {
				return migrations.ErrDrift
			}
		}
	}
	state, err := contract.ReadContract(ctx, s.Config.TenantID, s.Config.ObjectType, uuid.Nil, uuid.Nil)
	if err != nil {
		return err
	}
	var key mysqlstore.NullUUID
	schema, _ := mysqlstore.Identifier(s.Config.Database)
	return s.DB.QueryRowContext(ctx, "SELECT "+schema+".data_model_find_record_object_key_v1(?,?,?)", mysqlstore.UUID(s.Config.TenantID), mysqlstore.UUID(state.TableID), []byte("__readiness_nonmutating_probe__")).Scan(&key)
}
