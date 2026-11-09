package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	R "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/mysqlruntime"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

// This runs only inside the socket-mounted installer. All data-model mutations
// are performed by setup-mysql.ps1 through the owning service's HTTP API.
func configureLocalRuntime(ctx context.Context, db *sql.DB, ingestion migrations.Plan) error {
	tenant, err := uuid.Parse(os.Getenv("INGESTION_TENANT_ID"))
	if err != nil || tenant == uuid.Nil {
		return fmt.Errorf("INGESTION_TENANT_ID must identify the prepared local tenant")
	}
	raw, err := os.ReadFile("/plans/data_model.json")
	if err != nil {
		return err
	}
	var model migrations.Plan
	if err = json.Unmarshal(raw, &model); err != nil {
		return err
	}
	token, err := R.LocalSecret("ingestion_runtime_token")
	if err != nil {
		return err
	}
	c := R.Config{
		Authority: "local-mysql-development", Address: "mysql:3306", Database: localdev.Database,
		User: R.LocalAPIUser, Token: token, TenantID: tenant, ObjectType: os.Getenv("INGESTION_OBJECT_TYPE"),
		UploadQueue: "local_ingestion_uploads", DeferredQueue: "local_ingestion_deferred",
		// Keep API connections available for deferred admission, reads and
		// readiness while synchronous writers wait on receipt/record locks.
		Pool: 8, Concurrency: 1, WriteConcurrency: 4, Attempts: 3, BodyBytes: 1 << 20, ContractBytes: 1 << 20,
		Limits: store.RecordOwnerLimits{Receipt: store.ReceiptLimits{KeyBytes: 1024, ResultBytes: 1 << 20, CollisionRows: 16}, CommandBytes: 1 << 20, WriteBytes: 1 << 20, EffectRowBytes: 1 << 20, EffectBatchBytes: 4 << 20},
		Lease:  30 * time.Second, Timeout: 10 * time.Second, Poll: time.Second, Shutdown: 20 * time.Second,
		IngestionPlan: ingestion, DataModelPlan: model,
	}
	if err := c.Validate(); err != nil {
		return err
	}
	contracts, err := store.NewIngestionContractStore(db, localdev.Database, c.ContractBytes)
	if err != nil {
		return err
	}
	state, err := contracts.ReadContract(ctx, tenant, c.ObjectType, uuid.Nil, uuid.Nil)
	if err != nil {
		return err
	}
	if state.TenantStatus != "active" || state.TableID == uuid.Nil {
		return fmt.Errorf("an active tenant and published table are required")
	}
	var physical string
	if err := db.QueryRowContext(ctx, "SELECT physical_name FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=? AND write_state='open' AND published_version=?", mysqlstore.UUID(tenant), mysqlstore.UUID(state.TableID), mysqlstore.UUID(state.Version)).Scan(&physical); err != nil {
		return err
	}
	object, err := mysqlstore.Identifier(physical)
	if err != nil {
		return err
	}
	for _, worker := range []bool{false, true} {
		user, secret := R.LocalAPIUser, "ingestion_api_password"
		if worker {
			user, secret = R.LocalWorkerUser, "ingestion_worker_password"
		}
		password, err := R.LocalSecret(secret)
		if err != nil {
			return err
		}
		// Names are constants and passwords are validated hex before SQL binding.
		account := "'" + user + "'@'%'"
		statements := []string{
			"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + password + "'",
			"ALTER USER " + account + " IDENTIFIED BY '" + password + "'",
			"REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account,
		}
		grants, err := store.RuntimeGrants(localdev.Database, user, worker)
		if err != nil {
			return err
		}
		statements = append(statements, grants...)
		grants, err = store.PrivateExecutionGrants(localdev.Database, user)
		if err != nil {
			return err
		}
		statements = append(statements, grants...)
		statements = append(statements,
			"GRANT EXECUTE ON FUNCTION fraud.data_model_find_ingestion_contract_key_v1 TO "+account,
			"GRANT SELECT,INSERT,UPDATE ON fraud."+object+" TO "+account,
		)
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				// Do not log SQL containing the generated account password.
				return fmt.Errorf("configure %s: %w", user, err)
			}
		}
	}
	if err := configureVelocity(ctx, db, c, state.TableID, state.Version); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("/runtime", ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), "/runtime/config.json"); err != nil {
		return err
	}
	fmt.Printf("local ingestion scope: tenant=%s object_type=%s\n", tenant, c.ObjectType)
	return nil
}
