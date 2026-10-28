package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestConstraintInspectionValidation(t *testing.T) {
	for _, names := range [][2]string{{"", "t"}, {"db", "t;DROP"}, {"db.other", "t"}} {
		if _, err := InspectVisibleForeignKeyParts(context.Background(), &sql.Conn{}, names[0], names[1]); err == nil {
			t.Fatal("unsafe FK identifier accepted")
		}
		if _, err := InspectVisibleChecks(context.Background(), &sql.Conn{}, names[0], names[1]); err == nil {
			t.Fatal("unsafe check identifier accepted")
		}
	}
	if _, err := InspectVisibleForeignKeyParts(context.Background(), nil, "db", "t"); err == nil {
		t.Fatal("nil FK connection accepted")
	}
	if _, err := InspectVisibleChecks(context.Background(), nil, "db", "t"); err == nil {
		t.Fatal("nil check connection accepted")
	}
}

func TestMySQLConstraintInspection(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name := fmt.Sprintf("constraint_%d", time.Now().UnixNano())
	parentName := name + "_parent"
	parent, _ := Identifier(parentName)
	child, _ := Identifier(name)
	fkName, ckName := name+"_fk", name+"_ck"
	fk, _ := Identifier(fkName)
	ck, _ := Identifier(ckName)
	err := WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
		var database string
		if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
			return err
		}
		if p, err := InspectVisibleForeignKeyParts(ctx, conn, database, name); err != nil || len(p) != 0 {
			return fmt.Errorf("missing-table FK result: %v %v", p, err)
		}
		if c, err := InspectVisibleChecks(ctx, conn, database, name); err != nil || len(c) != 0 {
			return fmt.Errorf("missing-table check result: %v %v", c, err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+parent+" (tenant INT NOT NULL, id INT NOT NULL, PRIMARY KEY(tenant,id)) ENGINE=InnoDB"); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+child+" (id INT PRIMARY KEY, tenant INT NOT NULL, parent_id INT NOT NULL, score INT NOT NULL, CONSTRAINT "+fk+" FOREIGN KEY(tenant,parent_id) REFERENCES "+parent+"(tenant,id) ON UPDATE CASCADE ON DELETE RESTRICT, CONSTRAINT "+ck+" CHECK(score >= 0) NOT ENFORCED) ENGINE=InnoDB"); err != nil {
			return err
		}
		parts, err := InspectVisibleForeignKeyParts(ctx, conn, database, name)
		if err != nil {
			return err
		}
		if len(parts) != 2 {
			return fmt.Errorf("FK part count: %d", len(parts))
		}
		for i, p := range parts {
			if p.Name != fkName || p.Position != i+1 || !p.ReferencedPosition.Valid || p.ReferencedPosition.Int64 != int64(i+1) || p.ReferencedSchema != database || p.ReferencedTable != parentName || p.Column != []string{"tenant", "parent_id"}[i] || p.ReferencedColumn != []string{"tenant", "id"}[i] || p.UpdateRule != "CASCADE" || p.DeleteRule != "RESTRICT" || p.MatchOption != "NONE" {
				return fmt.Errorf("FK mapping lost: %+v", p)
			}
		}
		checks, err := InspectVisibleChecks(ctx, conn, database, name)
		if err != nil {
			return err
		}
		if len(checks) != 1 || checks[0].Name != ckName || checks[0].Clause == "" || checks[0].Enforced {
			return fmt.Errorf("check definition lost: %+v", checks)
		}
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+child+" ALTER CHECK "+ck+" ENFORCED"); err != nil {
			return err
		}
		changed, err := InspectVisibleChecks(ctx, conn, database, name)
		if err != nil {
			return err
		}
		if len(changed) != 1 || !changed[0].Enforced || changed[0].Clause != checks[0].Clause || checks[0].Enforced {
			return fmt.Errorf("enforcement drift lost: %+v", changed)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO "+parent+" VALUES (1,10)"); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO "+child+" VALUES (1,1,10,-1)"); Classify(err) != CheckViolation {
			return fmt.Errorf("enforced check accepted invalid data: %v", err)
		}
		// Test-only empty table under our DDL lock. A production replacement
		// needs a write barrier and recovery for this unconstrained interval.
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+child+" DROP FOREIGN KEY "+fk); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+child+" ADD CONSTRAINT "+fk+" FOREIGN KEY(tenant,parent_id) REFERENCES "+parent+"(tenant,id) ON UPDATE RESTRICT ON DELETE CASCADE"); err != nil {
			return err
		}
		revised, err := InspectVisibleForeignKeyParts(ctx, conn, database, name)
		if err != nil {
			return err
		}
		if len(revised) != 2 || revised[0].UpdateRule != "RESTRICT" || revised[0].DeleteRule != "CASCADE" || parts[0].UpdateRule != "CASCADE" {
			return fmt.Errorf("referential action drift lost: %+v", revised)
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		if got, err := InspectVisibleForeignKeyParts(canceled, conn, database, name); !errors.Is(err, context.Canceled) || got != nil {
			return fmt.Errorf("canceled FK read: %v %v", got, err)
		}
		if got, err := InspectVisibleChecks(canceled, conn, database, name); !errors.Is(err, context.Canceled) || got != nil {
			return fmt.Errorf("canceled check read: %v %v", got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
