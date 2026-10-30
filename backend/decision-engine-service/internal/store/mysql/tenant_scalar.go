package mysql

import (
	"encoding/json"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// QueryRecords is a text HTTP-shaped API. AggregateFilter values, in contrast,
// must already have the correct JSON/runtime type: SQL must not coerce "abc"
// into zero or turn malformed values into a successful no-match.
func (f registryField) queryText(text string) (any, error) {
	switch f.Type {
	case "string", "timestamp", "ip_address":
		return text, nil
	case "int":
		n, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return nil, ErrAggregateCapability
		}
		return n, nil
	case "float":
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, ErrAggregateCapability
		}
		return n, nil
	case "bool":
		if text == "true" {
			return true, nil
		}
		if text == "false" {
			return false, nil
		}
		return nil, ErrAggregateCapability
	default:
		return nil, ErrAggregateCapability
	}
}
func (f registryField) encode(value any) (any, error) {
	if value == nil {
		return nil, ErrAggregateCapability
	}
	switch f.Type {
	case "string":
		s, ok := value.(string)
		if !ok {
			return nil, ErrAggregateCapability
		}
		if _, err := (mysqlstore.Text(s)).Value(); err != nil {
			return nil, ErrAggregateCapability
		}
		return mysqlstore.Text(s), nil
	case "bool":
		b, ok := value.(bool)
		if !ok {
			return nil, ErrAggregateCapability
		}
		return mysqlstore.Bool(b), nil
	case "int":
		var n int64
		switch v := value.(type) {
		case int:
			n = int64(v)
		case int32:
			n = int64(v)
		case int64:
			n = v
		case json.Number:
			parsed, err := v.Int64()
			if err != nil {
				return nil, ErrAggregateCapability
			}
			n = parsed
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || v < math.MinInt32 || v > math.MaxInt32 {
				return nil, ErrAggregateCapability
			}
			n = int64(v)
		default:
			return nil, ErrAggregateCapability
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return nil, ErrAggregateCapability
		}
		return mysqlstore.Int32(n), nil
	case "float":
		var n float64
		var err error
		switch v := value.(type) {
		case float64:
			n = v
		case float32:
			n = float64(v)
		case int:
			n = float64(v)
		case int32:
			n = float64(v)
		case int64:
			n = float64(v)
		case json.Number:
			n, err = v.Float64()
		default:
			return nil, ErrAggregateCapability
		}
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, ErrAggregateCapability
		}
		return mysqlstore.Float64(n), nil
	case "timestamp":
		var t time.Time
		switch v := value.(type) {
		case time.Time:
			t = v
		case string:
			parsed, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				return nil, ErrAggregateCapability
			}
			t = parsed
		default:
			return nil, ErrAggregateCapability
		}
		at := mysqlstore.DateTime{Time: t}
		if _, err := at.Value(); err != nil {
			return nil, ErrAggregateCapability
		}
		return at, nil
	case "ip_address":
		s, ok := value.(string)
		if !ok {
			return nil, ErrAggregateCapability
		}
		prefix, err := netip.ParsePrefix(s)
		if err != nil {
			address, e := netip.ParseAddr(s)
			if e != nil {
				return nil, ErrAggregateCapability
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		encoded, err := mysqlstore.EncodeInet(prefix)
		if err != nil {
			return nil, ErrAggregateCapability
		}
		return encoded, nil
	default:
		return nil, ErrAggregateCapability
	}
}
func (f registryField) decode(raw []any) (any, error) {
	if f.Type == "ip_address" {
		var ip mysqlstore.NullInet
		if err := ip.ScanColumns(raw[0], raw[1], raw[2]); err != nil {
			return nil, err
		}
		if !ip.Valid {
			if !f.Nullable {
				return nil, ErrPublishedModelUnavailable
			}
			return nil, nil
		}
		if ip.Prefix.Bits() == ip.Prefix.Addr().BitLen() {
			return ip.Prefix.Addr().String(), nil
		}
		return ip.Prefix.String(), nil
	}
	if raw[0] == nil {
		if !f.Nullable {
			return nil, ErrPublishedModelUnavailable
		}
		return nil, nil
	}
	switch f.Type {
	case "string":
		var v mysqlstore.Text
		err := v.Scan(raw[0])
		return string(v), err
	case "bool":
		var v mysqlstore.Bool
		err := v.Scan(raw[0])
		return bool(v), err
	case "int":
		var v mysqlstore.Int32
		err := v.Scan(raw[0])
		return json.Number(strconv.FormatInt(int64(v), 10)), err
	case "float":
		var v mysqlstore.Float64
		err := v.Scan(raw[0])
		return json.Number(strconv.FormatFloat(float64(v), 'g', -1, 64)), err
	case "timestamp":
		var v mysqlstore.DateTime
		err := v.Scan(raw[0])
		return v.Time.Format(time.RFC3339Nano), err
	default:
		return nil, ErrPublishedModelUnavailable
	}
}

func (f registryField) predicate(op string, value any) (string, []any, error) {
	column := f.columns["value"]
	if f.Type == "ip_address" {
		column = f.columns["family"]
	}
	switch op {
	case "is_null":
		return column + " IS NULL", nil, nil
	case "is_not_null":
		return column + " IS NOT NULL", nil, nil
	}
	if op == "in" {
		values, ok := value.([]any)
		if !ok || len(values) > 100 {
			return "", nil, ErrAggregateCapability
		}
		if len(values) == 0 {
			return "FALSE", nil, nil
		}
		expressions := make([]string, 0, len(values))
		args := []any{}
		for _, v := range values {
			if v == nil {
				return "", nil, ErrAggregateCapability
			}
			expr, bound, err := f.predicate("eq", v)
			if err != nil {
				return "", nil, err
			}
			expressions = append(expressions, "("+expr+")")
			args = append(args, bound...)
		}
		return "(" + strings.Join(expressions, " OR ") + ")", args, nil
	}
	typed, err := f.encode(value)
	if err != nil {
		return "", nil, err
	}
	if f.Type == "ip_address" {
		// Equality is full inet identity (family/address/prefix), not text spelling.
		// PostgreSQL inet ordering is not emulated by lexicographic byte ordering.
		if op != "eq" && op != "neq" {
			return "", nil, ErrAggregateCapability
		}
		ip := typed.(mysqlstore.InetColumns)
		expr := "(" + f.columns["family"] + "=? AND " + f.columns["address"] + "=? AND " + f.columns["prefix"] + "=?)"
		if op == "neq" {
			expr = "NOT " + expr
		}
		return expr, []any{ip.Family, ip.Address, ip.PrefixBits}, nil
	}
	sqlOp := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[op]
	if op == "starts_with" {
		if f.Type != "string" {
			return "", nil, ErrAggregateCapability
		}
		s := string(typed.(mysqlstore.Text))
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
		return column + " LIKE ? COLLATE utf8mb4_0900_bin ESCAPE '!'", []any{mysqlstore.Text(escaped + "%")}, nil
	}
	if sqlOp == "" {
		return "", nil, ErrAggregateCapability
	}
	if f.Type == "string" {
		if op == "eq" && f.textEqualityHash != "" {
			expression, args := f.textEqualityPredicate(typed.(mysqlstore.Text))
			return expression, args, nil
		}
		column = "CAST(" + column + " AS BINARY)"
		return column + sqlOp + "CAST(? AS BINARY)", []any{typed}, nil
	}
	return column + sqlOp + "?", []any{typed}, nil
}
