package mysql

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"testing"
)

func reconcileProcessFixture(t *testing.T, f recordFixtureState) {
	t.Helper()
	rows, e := f.db.Query("SELECT HEX(r.id),j.state,j.attempt,(SELECT COUNT(*) FROM ingestion_work_reservations x WHERE x.tenant_id=r.tenant_id AND x.resource_id=r.id) FROM ingestion_work_resources r LEFT JOIN ingestion_work_jobs j ON j.tenant_id=r.tenant_id AND j.resource_id=r.id AND j.id=COALESCE(r.current_job_id,r.id) WHERE r.tenant_id=? AND r.table_id=? ORDER BY r.id LIMIT 1001", mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(f.model.Tables["records"].ID))
	if e != nil {
		t.Error(e)
		return
	}
	defer rows.Close()
	type outcome struct {
		ID, State              string
		Attempts, Reservations int
		Disposition            string
	}
	outcomes := []outcome{}
	for rows.Next() {
		var r outcome
		if e = rows.Scan(&r.ID, &r.State, &r.Attempts, &r.Reservations); e != nil {
			t.Error(e)
			return
		}
		switch r.State {
		case "completed", "discarded", "cancelled":
			r.Disposition = "terminal"
			if r.Reservations != 0 {
				t.Error("terminal work retained active reservation", r)
			}
		case "available", "running":
			r.Disposition = "explicitly_pending_fixture_boundary"
			if t.Name() != "TestProcessAPIRuntimeGrants" && t.Name() != "TestProcessScopedWorkerClaims" {
				t.Error("unexpected pending accepted work", r)
			}
		default:
			t.Error("unclassified accepted work", r)
		}
		outcomes = append(outcomes, r)
	}
	if e = rows.Err(); e != nil {
		t.Error(e)
	}
	if len(outcomes) > 1000 {
		t.Error("reconciliation bound exceeded")
	}
	data, e := json.Marshal(struct {
		Test     string
		Counts   fullOutcomeCounts
		Outcomes []outcome
	}{t.Name(), fullDurableCounts(t, f), outcomes})
	if e != nil {
		t.Error(e)
		return
	}
	t.Log("RECONCILIATION " + string(data))
}
