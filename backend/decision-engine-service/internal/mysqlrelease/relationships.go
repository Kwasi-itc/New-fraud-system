package mysqlrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// VerifyRelationships is an offline restore/cutover inspection, not a runtime
// readiness probe. Writers must be fenced by the caller. It checks every declared
// FK, including composite tenant keys; enabling foreign_key_checks alone does
// not prove imported rows are valid. Full child scans can be expensive: callers
// must supply a deadline and measure this on production-sized backups.
// It performs no mutation and does not validate relationships absent from DDL.
// Use an inspection account with visibility and SELECT on the complete reviewed
// schema inventory; partial metadata visibility cannot establish completeness.
func VerifyRelationships(ctx context.Context, db *sql.DB, database string) (int, error) {
	if db == nil {
		return 0, errors.New("inspection database required")
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME,CONSTRAINT_NAME,COLUMN_NAME,
 REFERENCED_TABLE_SCHEMA,REFERENCED_TABLE_NAME,REFERENCED_COLUMN_NAME
 FROM information_schema.KEY_COLUMN_USAGE
 WHERE TABLE_SCHEMA=? AND REFERENCED_TABLE_NAME IS NOT NULL
 ORDER BY TABLE_NAME,CONSTRAINT_NAME,ORDINAL_POSITION LIMIT 4097`, database)
	if err != nil {
		return 0, err
	}
	type relation struct {
		table, name, parentSchema, parent string
		columns, parents                  []string
	}
	var relations []relation
	count := 0
	for rows.Next() {
		var table, name, column, parentSchema, parent, parentColumn string
		if err := rows.Scan(&table, &name, &column, &parentSchema, &parent, &parentColumn); err != nil {
			rows.Close()
			return 0, err
		}
		count++
		if count > 4096 {
			rows.Close()
			return 0, errors.New("relationship inventory exceeds reviewed inspection bound")
		}
		if len(relations) == 0 || relations[len(relations)-1].table != table || relations[len(relations)-1].name != name {
			relations = append(relations, relation{table: table, name: name, parentSchema: parentSchema, parent: parent})
		}
		r := &relations[len(relations)-1]
		r.columns = append(r.columns, column)
		r.parents = append(r.parents, parentColumn)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(relations) == 0 {
		return 0, errors.New("no declared relationships visible; verify schema and inspection grants")
	}
	for i, r := range relations {
		quoted := make([]string, 0, 3+2*len(r.columns))
		names := append([]string{r.table, r.parentSchema, r.parent}, r.columns...)
		names = append(names, r.parents...)
		for _, name := range names {
			q, err := mysqlstore.Identifier(name)
			if err != nil {
				return i, err
			}
			quoted = append(quoted, q)
		}
		var nonnull, equality []string
		for j := range r.columns {
			child := "c." + quoted[3+j]
			nonnull = append(nonnull, child+" IS NOT NULL")
			equality = append(equality, "p."+quoted[3+len(r.columns)+j]+"="+child)
		}
		query := "SELECT EXISTS(SELECT 1 FROM " + schema + "." + quoted[0] + " c WHERE " + strings.Join(nonnull, " AND ") +
			" AND NOT EXISTS(SELECT 1 FROM " + quoted[1] + "." + quoted[2] + " p WHERE " + strings.Join(equality, " AND ") + "))"
		var invalid bool
		if err := db.QueryRowContext(ctx, query).Scan(&invalid); err != nil {
			return i, fmt.Errorf("inspect relationship %s.%s: %w", r.table, r.name, err)
		}
		if invalid {
			return i, fmt.Errorf("broken relationship %s.%s", r.table, r.name)
		}
	}
	return len(relations), nil
}
