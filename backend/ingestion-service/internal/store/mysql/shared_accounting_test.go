package mysql

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"testing"
)

type fullOutcomeCounts struct{ Records, PublicReceipts, ExecutionReceipts, Audits, Outbox, Resources, Jobs int64 }

// Accounting is privileged test observation, never runtime access. Every query is
// scoped to the fixture's stable tenant/table; jobs are joined through resources.
func fullDurableCounts(t *testing.T, f recordFixtureState) fullOutcomeCounts {
	t.Helper()
	var c fullOutcomeCounts
	tenant, table := mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(f.model.Tables["records"].ID)
	if e := f.db.QueryRow("SELECT COUNT(*) FROM " + f.qualified).Scan(&c.Records); e != nil {
		t.Fatal(e)
	}
	for _, item := range []struct {
		name string
		dest *int64
	}{{"ingestion_request_receipts", &c.PublicReceipts}, {"ingestion_execution_receipts", &c.ExecutionReceipts}, {"ingestion_audit", &c.Audits}, {"ingestion_outbox_events", &c.Outbox}, {"ingestion_work_resources", &c.Resources}} {
		if e := f.db.QueryRow("SELECT COUNT(*) FROM "+f.database+"."+item.name+" WHERE tenant_id=? AND table_id=?", tenant, table).Scan(item.dest); e != nil {
			t.Fatal(e)
		}
	}
	if e := f.db.QueryRow("SELECT COUNT(*) FROM "+f.database+".ingestion_work_jobs j JOIN "+f.database+".ingestion_work_resources r ON r.tenant_id=j.tenant_id AND r.id=j.resource_id WHERE r.tenant_id=? AND r.table_id=?", tenant, table).Scan(&c.Jobs); e != nil {
		t.Fatal(e)
	}
	return c
}
