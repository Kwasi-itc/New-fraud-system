package mysqlrelease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestRestoreRelationshipInspectionRejectsOrphansAndTenantMismatch(t *testing.T) {
	db, name, _, _ := testsupport.DecisionMySQLRuntimeDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(name)
	for _, query := range []string{
		"CREATE TABLE " + schema + ".fixture_restore_parent(tenant INT NOT NULL,id INT NOT NULL,PRIMARY KEY(tenant,id))",
		"CREATE TABLE " + schema + ".fixture_restore_child(id INT PRIMARY KEY,tenant INT,parent INT,CONSTRAINT fixture_restore_fk FOREIGN KEY(tenant,parent) REFERENCES " + schema + ".fixture_restore_parent(tenant,id))",
		"INSERT INTO " + schema + ".fixture_restore_parent VALUES(1,10),(2,20)",
		"INSERT INTO " + schema + ".fixture_restore_child VALUES(1,1,10),(2,NULL,999),(3,1,NULL)",
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := VerifyRelationships(ctx, db, name); err != nil || n == 0 {
		t.Fatal(n, err)
	}
	for _, test := range []struct{ name, values string }{
		{"missing parent", "(4,1,999)"},
		{"foreign tenant parent", "(4,1,20)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Simulate an invalid bulk import on one pinned session, then reenable
			// checks before inspection: the session flag must not mask old orphans.
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(ctx, "SET SESSION foreign_key_checks=0"); err != nil {
				t.Fatal(err)
			}
			_, insertErr := conn.ExecContext(ctx, "INSERT INTO "+schema+".fixture_restore_child VALUES"+test.values)
			_, resetErr := conn.ExecContext(ctx, "SET SESSION foreign_key_checks=1")
			if insertErr != nil || resetErr != nil {
				t.Fatal(insertErr, resetErr)
			}
			_, err = VerifyRelationships(ctx, db, name)
			if err == nil || !strings.Contains(err.Error(), "fixture_restore_fk") {
				t.Fatal("invalid restored relationship accepted", err)
			}
			if _, err := conn.ExecContext(ctx, "DELETE FROM "+schema+".fixture_restore_child WHERE id=4"); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyRelationships(ctx, db, name); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := VerifyRelationships(ctx, db, "missing_restore_schema"); err == nil {
		t.Fatal("empty inspection accepted")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := VerifyRelationships(canceled, db, name); err == nil {
		t.Fatal("canceled inspection accepted")
	}
}
