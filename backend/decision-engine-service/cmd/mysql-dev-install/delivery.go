package main

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlworkflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localdelivery"
	R "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

func installDeliveries(ctx context.Context) (plan migrations.Plan, err error) {
	steps, err := localdelivery.Statements("fraud", "local_delivery_guard")
	if err != nil {
		return plan, err
	}
	err = localdev.Install(ctx, localdev.Installation{Owner: localdelivery.Owner, Steps: steps, Accounts: []string{"local_delivery_guard"}, Grants: []string{"GRANT SELECT,TRIGGER ON fraud.decision_local_delivery_receipts TO `local_delivery_guard`@`localhost`"}, Upgrade: extendLocalOwnerPlan(steps), Verify: func(_ context.Context, _ *sql.DB, p migrations.Plan) error { plan = p; return nil }})
	return plan, err
}
func configureProviderAccount(ctx context.Context, db *sql.DB, host string) error {
	password, err := R.LocalSecret("decision_provider_password")
	if err != nil {
		return err
	}
	if _, err := R.LocalSecret("decision_provider_token"); err != nil {
		return err
	}
	account := "`" + R.LocalProviderUser + "`@'" + host + "'"
	statements := []string{"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + password + "'", "ALTER USER " + account + " IDENTIFIED BY '" + password + "'", "REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account}
	grants, err := mysqlworkflow.RuntimeGrants("fraud", R.LocalProviderUser)
	if err != nil {
		return err
	}
	for _, g := range grants {
		statements = append(statements, strings.ReplaceAll(g, "@'%'", "@'"+host+"'"))
	}
	for _, table := range []string{"data_model_tenants", "decision_local_schema_migrations", "decision_engine_schema_migrations", "screening_decision_source_v1", "decision_engine_decisions", "decision_engine_scoring_requests", "decision_engine_dispatch_intents", "decision_engine_effect_job_receipts"} {
		statements = append(statements, "GRANT SELECT ON fraud."+table+" TO "+account)
	}
	for _, table := range []string{"screening_screenings", "screening_matches", "screening_execution_projection_queue", "screening_intake_keys", "screening_intake_receipts"} {
		statements = append(statements, "GRANT SELECT,INSERT,UPDATE ON fraud."+table+" TO "+account)
	}
	statements = append(statements, "GRANT DELETE ON fraud.screening_matches TO "+account, "GRANT DELETE ON fraud.screening_execution_projection_queue TO "+account, "GRANT UPDATE(id) ON fraud.screening_decision_source_v1 TO "+account, "GRANT SELECT,UPDATE ON fraud.decision_engine_screening_executions TO "+account, "GRANT UPDATE ON fraud.decision_engine_scoring_requests TO "+account, "GRANT UPDATE(id) ON fraud.decision_engine_effect_job_receipts TO "+account, "GRANT SELECT,INSERT,UPDATE ON fraud.decision_local_delivery_receipts TO "+account)
	for _, q := range statements {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
