package mysql

import (
	"errors"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrRecordDecode = errors.New("retained record value violates physical type contract")

// DecodeRecordValue decodes database/sql values from an authorized mapped
// projection. Scalars require [value]; IP requires [address,family,prefix].
// Returns bool, int64, float64, string, UTC time.Time, netip.Prefix or nil.
// It does not authorize access or define the public HTTP representation.
// maxStringBytes bounds decoding only: the future SELECT must bound transfer
// before Scan, and the row reader must enforce an aggregate result budget.
func DecodeRecordValue(field ingestion.FieldSchema, parts []any, maxStringBytes int) (any, error) {
	if field.Archived || maxStringBytes < 1 || maxStringBytes > 16<<20 {
		return nil, ErrRecordDecode
	}
	count := 1
	switch field.DataType {
	case "bool", "int", "float", "string", "timestamp":
	case "ip_address":
		count = 3
	default:
		return nil, ErrRecordDecode
	}
	if len(parts) != count {
		return nil, ErrRecordDecode
	}
	nulls := 0
	for _, p := range parts {
		if p == nil {
			nulls++
		}
	}
	if nulls > 0 {
		if nulls == count && field.Nullable {
			return nil, nil
		}
		return nil, ErrRecordDecode
	}
	switch field.DataType {
	case "bool":
		n, err := recordInteger(parts[0])
		if err != nil || (n != 0 && n != 1) {
			return nil, ErrRecordDecode
		}
		return n == 1, nil
	case "int":
		n, err := recordInteger(parts[0])
		if err != nil || n < math.MinInt32 || n > math.MaxInt32 {
			return nil, ErrRecordDecode
		}
		return n, nil
	case "float":
		var n float64
		switch v := parts[0].(type) {
		case float64:
			n = v
		case []byte:
			if len(v) > 64 {
				return nil, ErrRecordDecode
			}
			var err error
			n, err = strconv.ParseFloat(string(v), 64)
			if err != nil {
				return nil, ErrRecordDecode
			}
		default:
			return nil, ErrRecordDecode
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, ErrRecordDecode
		}
		return n, nil
	case "string":
		switch v := parts[0].(type) {
		case string:
			if len(v) > maxStringBytes || !utf8.ValidString(v) {
				return nil, ErrRecordDecode
			}
			return v, nil
		case []byte:
			if len(v) > maxStringBytes || !utf8.Valid(v) {
				return nil, ErrRecordDecode
			}
			return string(v), nil
		default:
			return nil, ErrRecordDecode
		}
	case "timestamp":
		var at mysqlstore.DateTime
		if err := at.Scan(parts[0]); err != nil {
			return nil, ErrRecordDecode
		}
		return at.Time, nil
	case "ip_address":
		address, ok := parts[0].([]byte)
		if !ok || len(address) > 16 {
			return nil, ErrRecordDecode
		}
		family, err := recordInteger(parts[1])
		if err != nil || (family != 4 && family != 6) {
			return nil, ErrRecordDecode
		}
		prefix, err := recordInteger(parts[2])
		if err != nil || prefix < 0 || prefix > 128 {
			return nil, ErrRecordDecode
		}
		result, err := (mysqlstore.InetColumns{Address: address, Family: int(family), PrefixBits: int(prefix)}).Prefix()
		if err != nil {
			return nil, ErrRecordDecode
		}
		return result, nil
	}
	return nil, ErrRecordDecode
}

func recordInteger(raw any) (int64, error) {
	switch v := raw.(type) {
	case int64:
		return v, nil
	case uint64:
		if v <= math.MaxInt64 {
			return int64(v), nil
		}
	case []byte:
		if len(v) > 20 {
			return 0, ErrRecordDecode
		}
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err == nil {
			return n, nil
		}
	}
	return 0, ErrRecordDecode
}
