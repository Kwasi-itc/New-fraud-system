package mysql

import (
	"context"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
)

func TestIngestionWorkRestrictedGrants(t *testing.T) {
	f, w := workFixture(t)
	s := w.owner.receipts.schema
	for _, table := range []string{"data_model_tenants", "data_model_ingestion_contracts"} {
		if _, err := f.db.Exec("CREATE TABLE " + s + "." + table + "(id BINARY(16) PRIMARY KEY)"); err != nil {
			t.Fatal(err)
		}
	}
	pool := restrictedReceiptPool(t, f.db, f.database)
	var login string
	if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	role := strings.Split(login, "@")[0]
	grants, err := RuntimeGrants(f.database, role, true)
	if err != nil {
		t.Fatal(err)
	}
	grants = append(grants, "GRANT SELECT,INSERT,UPDATE ON "+f.qualified+" TO '"+role+"'@'%'")
	for _, grant := range grants {
		if _, err := f.db.Exec(grant); err != nil {
			t.Fatal(grant, err)
		}
	}
	w.owner.receipts.db = pool
	w.queue, err = work.New(pool, f.database, "ingestion", 4096)
	if err != nil {
		t.Fatal(err)
	}
	in := workInput(DeferredWork)
	ctx := context.Background()
	if _, err := w.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := w.Handle(ctx, claimWork(t, w, DeferredWork)); err != nil {
		t.Fatal(err)
	}
	result, err := w.Get(ctx, in.ID)
	if err != nil || result.State != "completed" {
		t.Fatal(result, err)
	}
	for _, query := range []string{
		"UPDATE " + s + ".ingestion_work_resources SET payload='{}'",
		"UPDATE " + s + ".ingestion_work_jobs SET payload='{}'",
		"UPDATE " + s + ".ingestion_execution_receipts SET response_payload='{}'",
		"DELETE FROM " + s + ".ingestion_work_jobs",
		"UPDATE " + s + ".ingestion_audit SET status='succeeded'",
		"SELECT * FROM " + s + ".data_model_exact_keys",
	} {
		if _, err := pool.Exec(query); err == nil {
			t.Fatal("forbidden mutation succeeded", query)
		}
	}
}
