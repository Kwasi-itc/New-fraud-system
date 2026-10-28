package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
)

// IndexPart describes one ordered key part, with its index-level attributes.
// Direction is A/D/NULL; PrefixLength is NULL for a full-column key part.
// Column is NULL for functional parts, whose Expression is present instead.
type IndexPart struct {
	Name, Type                    string
	Position                      int
	Unique, Visible               bool
	Column, Expression, Direction sql.NullString
	PrefixLength                  sql.NullInt64
}

// InspectVisibleIndexParts reads metadata visible to the connection's user,
// including optimizer-invisible indexes. Empty output does not prove the table
// exists or is index-free. Callers authorize names, establish metadata privileges
// and retain the DDL lock through inspection and any publication decision.
func InspectVisibleIndexParts(ctx context.Context, conn *sql.Conn, database, table string) ([]IndexPart, error) {
	if conn == nil || !identifier.MatchString(database) || !identifier.MatchString(table) {
		return nil, errors.New("connection and valid physical schema/table names required")
	}
	rows, err := conn.QueryContext(ctx, `SELECT INDEX_NAME, INDEX_TYPE, SEQ_IN_INDEX,
		NON_UNIQUE, IS_VISIBLE, COLUMN_NAME, EXPRESSION, COLLATION, SUB_PART
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=? AND TABLE_NAME=?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []IndexPart
	for rows.Next() {
		var p IndexPart
		var nonUnique int
		var visible string
		if err := rows.Scan(&p.Name, &p.Type, &p.Position, &nonUnique, &visible, &p.Column, &p.Expression, &p.Direction, &p.PrefixLength); err != nil {
			return nil, err
		}
		if (nonUnique != 0 && nonUnique != 1) || (visible != "YES" && visible != "NO") {
			return nil, errors.New("unexpected index uniqueness/visibility metadata")
		}
		p.Unique, p.Visible = nonUnique == 0, visible == "YES"
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return parts, nil
}
