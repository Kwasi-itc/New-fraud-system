// mysql-dev-install installs owner DDL only in the explicit local Docker stack.
package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"os"
	"time"
)

func run(ctx context.Context) error {
	p := localdev.Installation{Owner: "data_model"}
	builders := []func(string, installation.Definer) ([]migrations.Step, error){installation.Statements, installation.CreationReceiptUpgradeStatements, installation.OwnerResultUpgradeStatements, installation.RecordLookupUpgradeStatements, installation.ContractLookupUpgradeStatements}
	policies := []func(installation.Definer) installation.AccountPolicy{nil, installation.CreationReceiptDefinerPolicy, installation.OwnerResultDefinerPolicy, installation.RecordLookupDefinerPolicy, installation.ContractLookupDefinerPolicy}
	for i, name := range []string{"local_dm_guard", "local_dm_creation", "local_dm_result", "local_dm_record", "local_dm_contract"} {
		d := installation.Definer{User: name, Host: "localhost"}
		p.Accounts = append(p.Accounts, name)
		steps, err := builders[i](localdev.Database, d)
		if err != nil {
			return err
		}
		p.Steps = append(p.Steps, steps...)
		var grants []string
		if i == 0 {
			upgrade, err := installation.DeliveryRetryUpgradeStatement(localdev.Database)
			if err != nil {
				return err
			}
			p.Steps = append(p.Steps, upgrade)
			grants, err = installation.DefinerGrants(localdev.Database, d)
			if err != nil {
				return err
			}
		} else {
			grants, err = installation.GrantStatements(localdev.Database, policies[i](d))
			if err != nil {
				return err
			}
		}
		p.Grants = append(p.Grants, grants...)
	}
	workerIndexes, err := installation.WorkerIndexUpgradeStatements(localdev.Database)
	if err != nil {
		return err
	}
	p.Steps = append(p.Steps, workerIndexes...)
	p.Upgrade = installation.CaptureWorkerIndexUpgrade
	p.Verify = func(ctx context.Context, db *sql.DB, plan migrations.Plan) error {
		defs := make([]installation.Definer, 0, len(p.Accounts))
		for _, account := range p.Accounts {
			defs = append(defs, installation.Definer{User: account, Host: "localhost"})
		}
		ready, err := installation.NewExactKeyReadinessWithLookups(db, plan, defs[0], defs[1], defs[2], defs[3], defs[4])
		if err != nil {
			return err
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		// Checks shared prerequisites only, not publication of a business table.
		if err := ready.VerifyOnConn(ctx, conn, localdev.Database, installation.Definer{User: "local_future_record_guard", Host: "localhost"}); err != nil {
			return err
		}
		if os.Getenv("LOCAL_DATA_MODEL_API") != "1" {
			return nil
		}
		password, err := os.ReadFile("/run/secrets/data_model_mysql_password")
		if err != nil {
			return err
		}
		decoded, err := hex.DecodeString(string(password))
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("invalid local API secret")
		}
		// Only this installer has account administration rights. The API gets
		// owner metadata reads and the three inserts required for registration.
		for _, statement := range []string{
			"CREATE USER IF NOT EXISTS 'local_dm_api'@'%' IDENTIFIED BY '" + string(password) + "'",
			"ALTER USER 'local_dm_api'@'%' IDENTIFIED BY '" + string(password) + "'",
			"REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'local_dm_api'@'%'",
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("configure local API account: %w", err)
			}
		}
		seen := map[string]bool{}
		for _, step := range plan.Steps {
			if step.Kind != "TABLE" || seen[step.Name] {
				continue
			}
			seen[step.Name] = true
			if _, err := db.ExecContext(ctx, "GRANT SELECT ON `fraud`.`"+step.Name+"` TO 'local_dm_api'@'%'"); err != nil {
				return err
			}
		}
		for _, table := range []string{"data_model_tenants", "data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
			if _, err := db.ExecContext(ctx, "GRANT INSERT ON `fraud`.`"+table+"` TO 'local_dm_api'@'%'"); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx, "GRANT UPDATE (status,updated_at) ON `fraud`.`data_model_tenants` TO 'local_dm_api'@'%'"); err != nil {
			return err
		}
		return nil
	}
	return localdev.Install(ctx, p)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("data-model local MySQL migrations applied")
}
