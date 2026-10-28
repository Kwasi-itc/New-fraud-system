package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestMySQLTenantRelationshipIntegrity(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	suffix := time.Now().UnixNano()
	parent, _ := Identifier(fmt.Sprintf("integrity_parent_%d", suffix))
	child, _ := Identifier(fmt.Sprintf("integrity_child_%d", suffix))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+parent+" (tenant INT NOT NULL, id INT NOT NULL, PRIMARY KEY(tenant, id)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+child+" (tenant INT NOT NULL, id INT NOT NULL, parent_id INT NOT NULL, external_key INT NOT NULL, PRIMARY KEY(tenant,id), UNIQUE KEY (tenant,external_key), FOREIGN KEY(tenant,parent_id) REFERENCES "+parent+"(tenant,id)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+parent+" VALUES (1,10),(2,20)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+child+" VALUES (1,1,10,7),(2,1,20,7)"); err != nil {
		t.Fatalf("valid tenant-scoped relationships/unique keys failed: %v", err)
	}
	// A valid parent ID in another tenant must not satisfy this foreign key.
	err := Mutation(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+child+" VALUES (1,2,10,8)"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO "+child+" VALUES (1,3,20,9)")
		return err
	})
	if Classify(err) != ForeignKey {
		t.Fatalf("cross-tenant relationship was not rejected: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+child+" WHERE tenant=1 AND id=2").Scan(&count); err != nil || count != 0 {
		t.Fatalf("earlier write escaped transaction rollback: %d %v", count, err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+child+" VALUES (1,4,10,7)"); Classify(err) != Duplicate {
		t.Fatalf("secondary uniqueness not enforced: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM "+parent+" WHERE tenant=1 AND id=10"); Classify(err) != ForeignKey {
		t.Fatalf("referenced parent deleted: %v", err)
	}
	// Verify a following committed mutation is visible on another pool; normal
	// completion must not leave a chained or implicit transaction holding work.
	if err := Mutation(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO "+child+" VALUES (1,2,10,8)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reader := integrationDB(t)
	if err := reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+child+" WHERE tenant=1 AND id=2").Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed mutation not visible: %d %v", count, err)
	}
}
