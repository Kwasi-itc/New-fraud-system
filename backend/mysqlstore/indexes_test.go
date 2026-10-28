package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestIndexInspectionValidation(t *testing.T) {
	for _, names := range [][2]string{{"", "t"}, {"db", "t;DROP"}, {"db.other", "t"}} {
		if _, err := InspectVisibleIndexParts(context.Background(), &sql.Conn{}, names[0], names[1]); err == nil {
			t.Fatal("unsafe identifier accepted")
		}
	}
	if _, err := InspectVisibleIndexParts(context.Background(), nil, "db", "t"); err == nil {
		t.Fatal("nil connection accepted")
	}
}

func TestMySQLIndexInspection(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name := fmt.Sprintf("index_inspect_%d", time.Now().UnixNano())
	table, _ := Identifier(name)
	err := WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
		var database string
		if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
			return err
		}
		if parts, err := InspectVisibleIndexParts(ctx, conn, database, name); err != nil || len(parts) != 0 {
			return fmt.Errorf("unexpected missing-table metadata: %v %v", parts, err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+table+` (
			id INT PRIMARY KEY, tenant INT NOT NULL, label VARCHAR(30) NOT NULL, score INT NOT NULL,
			UNIQUE KEY by_tenant(tenant, label), KEY by_label(label(5) DESC) INVISIBLE,
			KEY by_score ((score + 1))
		) ENGINE=InnoDB`); err != nil {
			return err
		}
		parts, err := InspectVisibleIndexParts(ctx, conn, database, name)
		if err != nil {
			return err
		}
		byName := map[string][]IndexPart{}
		for i, p := range parts {
			if p.Type != "BTREE" || p.Position != len(byName[p.Name])+1 {
				return fmt.Errorf("invalid key-part order/type at %d: %+v", i, p)
			}
			byName[p.Name] = append(byName[p.Name], p)
		}
		if len(parts) != 5 || len(byName["PRIMARY"]) != 1 || len(byName["by_tenant"]) != 2 || len(byName["by_label"]) != 1 || len(byName["by_score"]) != 1 {
			return fmt.Errorf("unexpected indexes: %+v", parts)
		}
		pk, compound, prefix, functional := byName["PRIMARY"][0], byName["by_tenant"], byName["by_label"][0], byName["by_score"][0]
		if !pk.Unique || !pk.Visible || pk.Column.String != "id" || pk.PrefixLength.Valid {
			return fmt.Errorf("primary key metadata lost: %+v", pk)
		}
		if !compound[0].Unique || !compound[1].Unique || compound[0].Column.String != "tenant" || compound[1].Column.String != "label" || compound[1].PrefixLength.Valid {
			return fmt.Errorf("composite uniqueness/order lost: %+v", compound)
		}
		if prefix.Unique || prefix.Visible || prefix.Direction.String != "D" || !prefix.PrefixLength.Valid || prefix.PrefixLength.Int64 != 5 {
			return fmt.Errorf("prefix/direction/visibility lost: %+v", prefix)
		}
		if functional.Column.Valid || !functional.Expression.Valid || functional.Expression.String == "" {
			return fmt.Errorf("functional key lost: %+v", functional)
		}
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+table+" DROP INDEX by_label, ADD INDEX by_label(label(9) ASC) VISIBLE"); err != nil {
			return err
		}
		changed, err := InspectVisibleIndexParts(ctx, conn, database, name)
		if err != nil {
			return err
		}
		found := false
		for _, p := range changed {
			if p.Name == "by_label" {
				found = true
				if !p.Visible || p.Direction.String != "A" || p.PrefixLength.Int64 != 9 {
					return fmt.Errorf("same-name drift not observed: %+v", p)
				}
			}
		}
		if !found || prefix.Visible || prefix.PrefixLength.Int64 != 5 {
			return errors.New("missing updated index or mutated previous snapshot")
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		if got, err := InspectVisibleIndexParts(canceled, conn, database, name); !errors.Is(err, context.Canceled) || got != nil {
			return fmt.Errorf("canceled read returned index data: %v %v", got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
