package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestColumnInspectionValidation(t *testing.T) {
	for _, args := range [][2]string{{"", "table"}, {"db", "table;DROP"}, {"db.other", "table"}} {
		if _, err := InspectVisibleColumns(context.Background(), &sql.Conn{}, args[0], args[1]); err == nil {
			t.Fatal("unsafe name accepted")
		}
	}
	if _, err := InspectVisibleColumns(context.Background(), nil, "db", "table"); err == nil {
		t.Fatal("nil connection accepted")
	}
}

func TestMySQLColumnInspection(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name := fmt.Sprintf("inspect_%d", time.Now().UnixNano())
	table, _ := Identifier(name)
	err := WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
		var database string
		if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
			return err
		}
		if got, err := InspectVisibleColumns(ctx, conn, database, name); !errors.Is(err, ErrNoVisibleColumns) || got != nil {
			return fmt.Errorf("missing table result: %v %v", got, err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+table+` (
			id INT PRIMARY KEY, label VARCHAR(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
			revision INT NOT NULL DEFAULT 7, optional_value INT NULL,
			derived INT GENERATED ALWAYS AS (revision + 1) STORED
		) ENGINE=InnoDB`); err != nil {
			return err
		}
		columns, err := InspectVisibleColumns(ctx, conn, database, name)
		if err != nil {
			return err
		}
		if len(columns) != 5 {
			return fmt.Errorf("column count %d", len(columns))
		}
		for i, expected := range []string{"id", "label", "revision", "optional_value", "derived"} {
			if columns[i].Name != expected || columns[i].Ordinal != i+1 {
				return fmt.Errorf("column order changed: %+v", columns)
			}
		}
		label, revision, optional, derived := columns[1], columns[2], columns[3], columns[4]
		if label.Type != "varchar(30)" || label.Nullable || !label.Default.Valid || label.Default.String != "" || label.CharacterSet.String != "utf8mb4" || label.Collation.String != "utf8mb4_bin" {
			return fmt.Errorf("text column metadata lost: %+v", label)
		}
		if revision.Type != "int" || revision.Nullable || !revision.Default.Valid || revision.Default.String != "7" || revision.Collation.Valid || !optional.Nullable || optional.Default.Valid {
			return fmt.Errorf("numeric/null metadata lost: %+v %+v", revision, optional)
		}
		if !strings.Contains(derived.Extra, "STORED GENERATED") || derived.Expression == "" {
			return fmt.Errorf("generated expression lost: %+v", derived)
		}
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+table+" MODIFY revision BIGINT NULL DEFAULT 9"); err != nil {
			return err
		}
		changed, err := InspectVisibleColumns(ctx, conn, database, name)
		if err != nil {
			return err
		}
		if changed[2].Type != "bigint" || !changed[2].Nullable || changed[2].Default.String != "9" || columns[2].Type != "int" {
			return fmt.Errorf("same-name drift not observable: %+v", changed[2])
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		if got, err := InspectVisibleColumns(canceled, conn, database, name); !errors.Is(err, context.Canceled) || got != nil {
			return fmt.Errorf("canceled read returned metadata: %v %v", got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
