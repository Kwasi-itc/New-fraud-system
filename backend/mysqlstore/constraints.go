package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
)

type ForeignKeyPart struct {
	Name, Column, ReferencedSchema, ReferencedTable, ReferencedColumn string
	Position                                                          int
	ReferencedPosition                                                sql.NullInt64
	UpdateRule, DeleteRule, MatchOption                               string
}

type CheckDefinition struct {
	Name, Clause string
	Enforced     bool
}

// InspectVisibleForeignKeyParts returns ordered FK mappings visible to this
// account. Callers authorize names, hold the DDL lock and establish completeness
// of metadata privileges. Empty output does not establish table existence.
func InspectVisibleForeignKeyParts(ctx context.Context, conn *sql.Conn, database, table string) ([]ForeignKeyPart, error) {
	if conn == nil || !identifier.MatchString(database) || !identifier.MatchString(table) {
		return nil, errors.New("connection and valid physical schema/table names required")
	}
	rows, err := conn.QueryContext(ctx, `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME,
		k.REFERENCED_TABLE_SCHEMA, k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME,
		k.ORDINAL_POSITION, k.POSITION_IN_UNIQUE_CONSTRAINT, r.UPDATE_RULE, r.DELETE_RULE, r.MATCH_OPTION
		FROM information_schema.KEY_COLUMN_USAGE k
		JOIN information_schema.REFERENTIAL_CONSTRAINTS r
		ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME
		WHERE k.TABLE_SCHEMA=? AND k.TABLE_NAME=? AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.CONSTRAINT_NAME, k.ORDINAL_POSITION`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []ForeignKeyPart
	for rows.Next() {
		var p ForeignKeyPart
		if err := rows.Scan(&p.Name, &p.Column, &p.ReferencedSchema, &p.ReferencedTable, &p.ReferencedColumn, &p.Position, &p.ReferencedPosition, &p.UpdateRule, &p.DeleteRule, &p.MatchOption); err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return parts, nil
}

// InspectVisibleChecks retains server expression text and enforcement state.
// It has the same authorization, DDL-lock and metadata-visibility requirements
// as InspectVisibleForeignKeyParts. Clause text is not canonical application SQL.
func InspectVisibleChecks(ctx context.Context, conn *sql.Conn, database, table string) ([]CheckDefinition, error) {
	if conn == nil || !identifier.MatchString(database) || !identifier.MatchString(table) {
		return nil, errors.New("connection and valid physical schema/table names required")
	}
	rows, err := conn.QueryContext(ctx, `SELECT t.CONSTRAINT_NAME, c.CHECK_CLAUSE, t.ENFORCED
		FROM information_schema.TABLE_CONSTRAINTS t
		JOIN information_schema.CHECK_CONSTRAINTS c
		ON c.CONSTRAINT_SCHEMA=t.CONSTRAINT_SCHEMA AND c.CONSTRAINT_NAME=t.CONSTRAINT_NAME
		WHERE t.TABLE_SCHEMA=? AND t.TABLE_NAME=? AND t.CONSTRAINT_TYPE='CHECK'
		ORDER BY t.CONSTRAINT_NAME`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var checks []CheckDefinition
	for rows.Next() {
		var c CheckDefinition
		var enforced string
		if err := rows.Scan(&c.Name, &c.Clause, &enforced); err != nil {
			return nil, err
		}
		if enforced != "YES" && enforced != "NO" {
			return nil, errors.New("unexpected check enforcement metadata")
		}
		c.Enforced = enforced == "YES"
		checks = append(checks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return checks, nil
}
