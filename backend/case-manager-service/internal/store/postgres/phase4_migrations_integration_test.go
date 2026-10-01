package postgres_test

import "testing"

func TestPhase4ForwardAndRollbackMigrationOrder(t *testing.T) {
	db := testDatabase(t, false)
	for _, name := range []string{"000002_tenant_integrity", "000003_phase1_access", "000004_reliable_intake", "000005_workspace_indexes", "000006_worker_delivery", "000007_assignment_sla", "000008_evidence_uploads"} {
		applyMigration(t, db, name+".up.sql")
	}
	fixture(t, db)
	for _, name := range []string{"000008_evidence_uploads", "000007_assignment_sla", "000006_worker_delivery"} {
		applyMigration(t, db, name+".down.sql")
	}
	for _, name := range []string{"000006_worker_delivery", "000007_assignment_sla", "000008_evidence_uploads"} {
		applyMigration(t, db, name+".up.sql")
	}
}
