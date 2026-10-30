package mysql

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func ipOrderExpressions(field projectionField) []string {
	a, f, m := field.components[0], field.components[1], field.components[2]
	mask := "UNHEX(CONCAT(REPEAT('FF'," + m + " DIV 8),IF(MOD(" + m + ",8)=0,'',LPAD(HEX(256-POW(2,8-MOD(" + m + ",8))),2,'0')),REPEAT('00',OCTET_LENGTH(" + a + ")-CEIL(" + m + "/8))))"
	return []string{f, "(" + a + " & " + mask + ")", m, a}
}

func ipScalarPredicate(field projectionField, operator string, value any) (ScalarPredicate, error) {
	if field.width != 3 || len(field.components) != 3 || field.definition.Archived {
		return ScalarPredicate{}, ErrRecordPredicate
	}
	if operator == "is_null" {
		return ScalarPredicate{clause: field.column + " IS NULL"}, nil
	}
	if operator == "is_not_null" {
		return ScalarPredicate{clause: field.column + " IS NOT NULL"}, nil
	}
	symbol, ok := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[operator]
	if !ok {
		return ScalarPredicate{}, ErrRecordPredicate
	}
	args := []any{nil, nil, nil, nil}
	if value != nil {
		prefix, ok := value.(netip.Prefix)
		if !ok {
			return ScalarPredicate{}, ErrRecordPredicate
		}
		host, err := mysqlstore.EncodeInet(prefix)
		if err != nil {
			return ScalarPredicate{}, ErrRecordPredicate
		}
		network, err := mysqlstore.EncodeInet(prefix.Masked())
		if err != nil {
			return ScalarPredicate{}, ErrRecordPredicate
		}
		args = []any{host.Family, network.Address, host.PrefixBits, host.Address}
	}
	return ScalarPredicate{clause: "(" + strings.Join(ipOrderExpressions(field), ",") + ") " + symbol + " (?,?,?,?)", args: args}, nil
}

// IP order is family, network bits, prefix length, then complete host address.
// All expressions use the owner-validated components, never logical SQL names.
// This may sort the matching input; LIMIT bounds transfer, not evaluation cost.
func (p *RecordProjection) compileIPExtreme(field projectionField, operation string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (string, []any, error) {
	if field.width != 3 || len(field.components) != 3 || field.definition.Archived || (operation != "MIN" && operation != "MAX") {
		return "", nil, ErrRecordAggregate
	}
	compiled, err := p.CompileFilter(filter)
	if err != nil {
		return "", nil, err
	}
	where, args := compiled.SQL()
	a := field.components[0]
	direction := " ASC"
	if operation == "MAX" {
		direction = " DESC"
	}
	query := "SELECT " + strings.Join(field.components, ",") + " FROM " + p.table + " WHERE " + a + " IS NOT NULL"
	if where != "" {
		query += " AND (" + where + ")"
	}
	query += " ORDER BY " + strings.Join(ipOrderExpressions(field), direction+",") + direction + " LIMIT 1"
	if err = ValidateRecordQuery(query, args, limits); err != nil {
		return "", nil, err
	}
	return query, args, nil
}

func (p *RecordProjection) ipExtreme(ctx context.Context, tx *sql.Tx, field projectionField, operation string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (any, error) {
	query, args, err := p.compileIPExtreme(field, operation, filter, limits)
	if err != nil {
		return nil, err
	}
	var address, family, prefix any
	if err = tx.QueryRowContext(ctx, query, args...).Scan(&address, &family, &prefix); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return DecodeRecordValue(field.definition, []any{address, family, prefix}, 64)
}
