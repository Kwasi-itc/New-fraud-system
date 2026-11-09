package mysql

import (
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
)

// InstallationStatements is the ordered source manifest. Cross-owner data-model
// tenants/tables/physical registry and v005 lookup must be installed first.
// Runtime principals cannot run this manifest. All tables use one database.
func InstallationStatements(database string) ([]migrations.Step, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	var steps []migrations.Step
	add := func(id, name, sql string) {
		steps = append(steps, migrations.Step{ID: id, Kind: "TABLE", Name: name, SQL: sql})
	}
	receipts, err := ReceiptSchema(database)
	if err != nil {
		return nil, err
	}
	for i, name := range []string{"ingestion_receipt_buckets", "ingestion_request_receipts"} {
		add(fmt.Sprintf("ingestion_v001_%02d", len(steps)+1), name, receipts[i])
	}
	effects, err := EffectSchema(database)
	if err != nil {
		return nil, err
	}
	for i, name := range []string{"ingestion_audit", "ingestion_outbox_events"} {
		add(fmt.Sprintf("ingestion_v001_%02d", len(steps)+1), name, effects[i])
	}
	guard, err := LookupGuardSchema(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v001_05", "ingestion_lookup_guards", guard)
	work, err := WorkSchema(database)
	if err != nil {
		return nil, err
	}
	for i, name := range []string{"ingestion_work_resources", "ingestion_work_jobs", "ingestion_work_reservations", "ingestion_work_jobs", "ingestion_execution_buckets", "ingestion_execution_receipts"} {
		add(fmt.Sprintf("ingestion_v001_%02d", len(steps)+1), name, work[i])
	}
	// Retain lineage rather than allowing a table/tenant deletion to erase replay
	// protection or ingestion evidence. Retirement precedes any coordinated purge.
	for _, name := range []string{"ingestion_request_receipts", "ingestion_execution_receipts", "ingestion_audit", "ingestion_outbox_events"} {
		add(fmt.Sprintf("ingestion_v001_%02d", len(steps)+1), name, "ALTER TABLE "+s+"."+name+" ADD CONSTRAINT "+name+"_table_fk FOREIGN KEY(tenant_id,table_id) REFERENCES "+s+".data_model_tables(tenant_id,id)")
	}
	for _, name := range []string{"ingestion_receipt_buckets", "ingestion_execution_buckets"} {
		add(fmt.Sprintf("ingestion_v001_%02d", len(steps)+1), name, "ALTER TABLE "+s+"."+name+" ADD CONSTRAINT "+name+"_tenant_fk FOREIGN KEY(tenant_id) REFERENCES "+s+".data_model_tenants(id)")
	}
	legacy, err := LegacyImportSchema(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v001_legacy", "ingestion_legacy_imports", legacy)
	operator, err := WorkOperatorSchema(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v002_work_actions", "ingestion_work_actions", operator)
	inspection, err := WorkInspectionIndex(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v003_operator_history", "ingestion_work_jobs", inspection)
	metrics, err := WorkMetricsIndex(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v004_deferred_metrics", "ingestion_work_jobs", metrics)
	pointer, err := WorkRedrivePointer(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v005_current_job", "ingestion_work_resources", pointer)
	redrive, err := WorkRedriveSchema(database)
	if err != nil {
		return nil, err
	}
	add("ingestion_v005_work_redrives", "ingestion_work_redrives", redrive)
	velocitySQL, err := velocity.Schema(database)
	if err != nil {
		return nil, err
	}
	for i, name := range []string{"ingestion_velocity_definitions", "ingestion_velocity_buckets"} {
		add(fmt.Sprintf("ingestion_v006_velocity_%d", i+1), name, velocitySQL[i])
	}
	return steps, nil
}

// InstallationPlan binds exact shipped SQL to reviewed, pinned-server reference
// definitions. It never learns an acceptable schema from the target deployment.
// Keep the resulting plan immutable and archive it with its source revision.
func InstallationPlan(database string, reference []migrations.Step) (migrations.Plan, error) {
	expected, err := InstallationStatements(database)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(expected) != len(reference) {
		return migrations.Plan{}, errors.New("complete ingestion reference plan required")
	}
	for i, want := range expected {
		got := reference[i]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL {
			return migrations.Plan{}, errors.New("ingestion reference differs from source manifest")
		}
	}
	p := migrations.Plan{Database: database, Owner: "ingestion", Steps: append([]migrations.Step(nil), reference...)}
	if err := p.Validate(); err != nil {
		return migrations.Plan{}, err
	}
	return p, nil
}
