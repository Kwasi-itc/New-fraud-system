package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNoVisibleColumns = errors.New("no columns visible for requested table")

// ColumnDefinition contains column metadata visible to the connection's user.
// Default.Valid=false cannot distinguish DEFAULT NULL from no default clause.
// Indexes, constraints, grants and table options require separate inspection.
type ColumnDefinition struct {
	Name, Type              string
	Ordinal                 int
	Nullable                bool
	Default                 sql.NullString
	CharacterSet, Collation sql.NullString
	Extra, Expression       string
}

// InspectVisibleColumns reads one exact schema/table on the supplied connection.
// Callers authorize physical names and hold the appropriate DDL lock throughout
// inspection and subsequent decisions. Visibility is privilege-dependent; this
// helper alone cannot certify completeness, absence or publication readiness.
func InspectVisibleColumns(ctx context.Context, conn *sql.Conn, database, table string) ([]ColumnDefinition, error) {
	if conn == nil || !identifier.MatchString(database) || !identifier.MatchString(table) {
		return nil, errors.New("connection and valid physical schema/table names required")
	}
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME, COLUMN_TYPE, ORDINAL_POSITION,
		IS_NULLABLE, COLUMN_DEFAULT, CHARACTER_SET_NAME, COLLATION_NAME, EXTRA, GENERATION_EXPRESSION
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []ColumnDefinition
	for rows.Next() {
		var c ColumnDefinition
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &c.Ordinal, &nullable, &c.Default, &c.CharacterSet, &c.Collation, &c.Extra, &c.Expression); err != nil {
			return nil, err
		}
		if nullable != "YES" && nullable != "NO" {
			return nil, errors.New("unexpected column nullability metadata")
		}
		c.Nullable = nullable == "YES"
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, ErrNoVisibleColumns
	}
	return columns, nil
}
