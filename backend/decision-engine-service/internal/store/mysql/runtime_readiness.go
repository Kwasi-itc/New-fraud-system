package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

type RuntimeReadinessPolicy struct {
	ExpectedVersion, PoolName string
	InspectionPoolName        string
	ReservedConnections       int64
	Pools                     []mysqlstore.PoolBudget
}

// OwnerReadiness checks the real installed provider contract, not a boolean
// "enabled" flag. Checks must honor ctx and perform no mutations/activation.
// Readiness is not authorization: each subsequent action still reauthorizes.
type OwnerReadiness func(context.Context) error

type RuntimeReadiness struct {
	db               *sql.DB
	inspection       *sql.DB
	database, schema string
	runner           *migrations.Runner
	policy           RuntimeReadinessPolicy
	owners           map[string]OwnerReadiness
}

// NewRuntimeReadiness binds EXACT reviewed 149-step decision DDL, not a generic
// ping or a runner for another database. A release must supply pinned reference
// definitions; this function never learns definitions from the target or runs DDL.
func NewRuntimeReadiness(db, inspection *sql.DB, database, definer string, reference []migrations.Step, policy RuntimeReadinessPolicy, owners map[string]OwnerReadiness) (*RuntimeReadiness, error) {
	plan, err := metadata.DispatchPlan(database, definer, reference)
	if err != nil {
		return nil, err
	}
	runner, err := migrations.New(inspection, "decision_engine", plan)
	if err != nil {
		return nil, err
	}
	if db == nil || inspection == nil || policy.ExpectedVersion == "" || policy.PoolName == "" || policy.InspectionPoolName == "" || (db == inspection) != (policy.PoolName == policy.InspectionPoolName) || policy.ReservedConnections < 0 || len(policy.Pools) == 0 || len(policy.Pools) > 1024 {
		return nil, errors.New("explicit version, local pool identity and complete shared connection inventory required")
	}
	schema, _ := mysqlstore.Identifier(database)
	policy.Pools = append([]mysqlstore.PoolBudget{}, policy.Pools...)
	copyOwners := map[string]OwnerReadiness{}
	for _, name := range []string{"auth", "data_model", "case_management", "screening", "dispatch"} {
		copyOwners[name] = owners[name]
	}
	return &RuntimeReadiness{db: db, inspection: inspection, database: database, schema: schema, runner: runner, policy: policy, owners: copyOwners}, nil
}

// Check is a conservative deployment prerequisite, not permission to activate.
// Installer-visible migration inspection and runtime-pool access are deliberate
// separate deployment concerns. Production grants/foreign providers are NOT
// manufactured here. An omitted dependency always leaves this check not ready.
// Other pool inventory completeness cannot be discovered from one sql.DB.
func (r *RuntimeReadiness) Check(ctx context.Context) (mysqlstore.ConnectionBudget, error) {
	if r == nil || r.db == nil || r.runner == nil {
		return mysqlstore.ConnectionBudget{}, ErrDecisionAuthorizationUnavailable
	}
	for _, name := range []string{"auth", "data_model", "case_management", "screening", "dispatch"} {
		check := r.owners[name]
		if check == nil {
			return mysqlstore.ConnectionBudget{}, fmt.Errorf("%s: %w", name, ErrOwnerContractUnavailable)
		}
		if err := check(ctx); err != nil {
			return mysqlstore.ConnectionBudget{}, fmt.Errorf("%s readiness: %w", name, err)
		}
	}
	var database, version, server string
	var limit int64
	if err := r.db.QueryRowContext(ctx, "SELECT DATABASE(),VERSION(),@@max_connections,@@server_uuid").Scan(&database, &version, &limit, &server); err != nil {
		return mysqlstore.ConnectionBudget{}, err
	}
	if database != r.database || version != r.policy.ExpectedVersion {
		return mysqlstore.ConnectionBudget{}, errors.New("database or pinned version differs from deployment contract")
	}
	// Shape verification uses an explicitly separate inspection pool/role. Do
	// not grant installer metadata/trigger privileges to the API just to make
	// readiness pass. Both pools must target the same actual writer/database.
	var inspectionDatabase, inspectionServer string
	if err := r.inspection.QueryRowContext(ctx, "SELECT DATABASE(),@@server_uuid").Scan(&inspectionDatabase, &inspectionServer); err != nil {
		return mysqlstore.ConnectionBudget{}, err
	}
	if inspectionDatabase != database || inspectionServer != server {
		return mysqlstore.ConnectionBudget{}, errors.New("inspection and runtime pools target different database servers")
	}
	found := false
	maximum := int64(r.db.Stats().MaxOpenConnections)
	for _, pool := range r.policy.Pools {
		if pool.Name == r.policy.PoolName {
			found = true
			if maximum <= 0 || maximum != pool.MaxOpen {
				return mysqlstore.ConnectionBudget{}, errors.New("runtime pool maximum differs from shared budget inventory")
			}
		}
	}
	if !found {
		return mysqlstore.ConnectionBudget{}, errors.New("local pool missing from shared budget inventory")
	}
	inspectionFound := false
	for _, pool := range r.policy.Pools {
		if pool.Name == r.policy.InspectionPoolName {
			inspectionFound = true
			if maximum := int64(r.inspection.Stats().MaxOpenConnections); maximum <= 0 || maximum != pool.MaxOpen {
				return mysqlstore.ConnectionBudget{}, errors.New("inspection pool differs from shared budget inventory")
			}
		}
	}
	if !inspectionFound {
		return mysqlstore.ConnectionBudget{}, errors.New("inspection pool missing from shared budget inventory")
	}
	budget, err := mysqlstore.ValidateConnectionBudget(limit, r.policy.ReservedConnections, r.policy.Pools)
	if err != nil {
		return mysqlstore.ConnectionBudget{}, err
	}
	if err := r.runner.Verify(ctx, nil); err != nil {
		return mysqlstore.ConnectionBudget{}, err
	}
	for _, table := range []string{"decision_engine_effect_job_receipts", "decision_engine_execution_job_receipts", "decision_engine_effect_job_deliveries", "decision_engine_execution_job_deliveries", "decision_engine_dispatch_intents"} {
		rows, err := r.db.QueryContext(ctx, "SELECT * FROM "+r.schema+"."+table+" LIMIT 0")
		if err != nil {
			return mysqlstore.ConnectionBudget{}, err
		}
		readErr := rows.Err()
		closeErr := rows.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return mysqlstore.ConnectionBudget{}, err
		}
	}
	return budget, nil
}
