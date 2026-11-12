package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMySQLPrerequisitePrefixPreservesFullLineageAndDirtyRecovery(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	name, child, dependency := "prefix_"+suffix, "child_"+suffix, "dep_"+suffix
	plan := fixturePlan(t, db, name)
	refDep, refChild := dependency+"_ref", child+"_ref"
	for _, q := range []string{
		"CREATE TABLE migration_test." + refDep + "(id INT PRIMARY KEY) ENGINE=InnoDB",
		"CREATE TABLE migration_test." + refChild + "(id INT PRIMARY KEY,parent_id INT,FOREIGN KEY(parent_id) REFERENCES migration_test." + refDep + "(id)) ENGINE=InnoDB",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Definition(ctx, conn, "migration_test", "TABLE", refChild)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps = append(plan.Steps, Step{ID: "needs_external_owner", Kind: "TABLE", Name: child, SQL: "CREATE TABLE migration_test." + child + "(id INT PRIMARY KEY,parent_id INT,FOREIGN KEY(parent_id) REFERENCES migration_test." + dependency + "(id)) ENGINE=InnoDB", After: strings.ReplaceAll(strings.ReplaceAll(after, refChild, child), refDep, dependency)})
	owner := "prefix_" + suffix
	r, err := NewForOwner(db, owner, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyThrough(ctx, plan.Steps[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ctx); err == nil {
		t.Fatal("missing external prerequisite was ignored")
	}
	if err := r.ApplyThrough(ctx, plan.Steps[0].ID); err != nil {
		t.Fatal("could not verify completed prerequisite", err)
	}
	var count, dirty int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*),SUM(state='dirty') FROM migration_test."+owner+"_schema_migrations").Scan(&count, &dirty); err != nil || count != 2 || dirty != 1 {
		t.Fatal(count, dirty, err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE migration_test."+child+"(id INT PRIMARY KEY,unexpected INT) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyThrough(ctx, plan.Steps[0].ID); !errors.Is(err, ErrDrift) {
		t.Fatal("dirty suffix drift bypassed", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE migration_test."+child); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE migration_test."+dependency+"(id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ctx); err != nil {
		t.Fatal("dirty step did not resume", err)
	}
	if err := r.ApplyThrough(ctx, plan.Steps[0].ID); err != nil {
		t.Fatal("completed full ledger was truncated", err)
	}
	if err := r.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyThrough(ctx, "unknown_step"); err == nil {
		t.Fatal("unknown prerequisite accepted")
	}
	if _, err := db.ExecContext(ctx, "UPDATE migration_test."+owner+"_schema_migrations SET checksum=UNHEX(REPEAT('00',32)) WHERE step_no=2"); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyThrough(ctx, plan.Steps[0].ID); !errors.Is(err, ErrLineage) {
		t.Fatal("later journal checksum bypassed", err)
	}
}
