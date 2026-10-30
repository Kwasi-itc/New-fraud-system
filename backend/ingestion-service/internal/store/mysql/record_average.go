package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"math/big"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

var ErrRecordAverageResult = errors.New("AVG result violates numeric precision or accumulation contract")

// RecordDecimal retains a bounded decimal without conversion through float64.
// Integer AVG uses PostgreSQL 16 division scale and rounding from exact sum/count;
// it does not depend on MySQL's session div_precision_increment.
type RecordDecimal string

// AverageRecords returns RecordDecimal for int fields, float64 for float fields,
// and nil for empty/all-null input. Caller owns the originating projection's
// authorized READ COMMITTED transaction, publication barrier and deadline.
func (p *RecordProjection) AverageRecords(ctx context.Context, tx *sql.Tx, name string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (any, error) {
	if tx == nil {
		return nil, ErrRecordAggregate
	}
	query, args, field, err := p.compileAggregate(name, "AVG", filter, limits)
	if err != nil {
		return nil, err
	}
	var raw, rawCount, rawMax any
	dest := []any{&raw, &rawCount}
	if field.DataType == "float" {
		dest = append(dest, &rawMax)
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(dest...); err != nil {
		return nil, err
	}
	count, err := decodeRecordCount(rawCount)
	if err != nil {
		return nil, ErrRecordAverageResult
	}
	if count == 0 {
		if raw != nil {
			return nil, ErrRecordAverageResult
		}
		return nil, nil
	}
	if field.DataType == "int" {
		return integerRecordAverage(raw, count)
	}
	if !safeFloatAccumulation(count, rawMax) {
		return nil, ErrRecordAverageResult
	}
	value, err := decodeRecordSum("float", raw)
	if err != nil {
		return nil, ErrRecordAverageResult
	}
	return value, nil
}

// PostgreSQL 16 numeric.c select_div_scale selects at least 16 significant
// digits in base 10000. Both operands here have zero decimal scale. Retaining
// the source scale is necessary for repeating averages such as 1/3.
// Reference: https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/utils/adt/numeric.c
func integerRecordAverage(raw any, count int64) (RecordDecimal, error) {
	sum, err := recordInteger(raw)
	if err != nil || count <= 0 {
		return "", ErrRecordAverageResult
	}
	if sum == 0 {
		return RecordDecimal("0"), nil
	}
	average := new(big.Rat).SetFrac(big.NewInt(sum), big.NewInt(count))
	if average.Cmp(new(big.Rat).SetInt64(math.MinInt32)) < 0 || average.Cmp(new(big.Rat).SetInt64(math.MaxInt32)) > 0 {
		return "", ErrRecordAverageResult
	}
	leading := func(n int64) (int, int64) {
		v := new(big.Int).Abs(big.NewInt(n))
		base := big.NewInt(10000)
		weight := 0
		for v.Cmp(base) >= 0 {
			v.Quo(v, base)
			weight++
		}
		return weight, v.Int64()
	}
	sw, sd := leading(sum)
	cw, cd := leading(count)
	weight := sw - cw
	if sd <= cd {
		weight--
	}
	scale := 16 - 4*weight
	if scale < 0 || scale > 64 {
		return "", ErrRecordAverageResult
	}
	return decodeRecordAverageDecimal(average.FloatString(scale))
}
func decodeRecordAverageDecimal(raw any) (RecordDecimal, error) {
	var text string
	switch value := raw.(type) {
	case []byte:
		if len(value) > 64 {
			return "", ErrRecordAverageResult
		}
		text = string(value)
	case string:
		text = value
	default:
		return "", ErrRecordAverageResult
	}
	if len(text) < 1 || len(text) > 64 {
		return "", ErrRecordAverageResult
	}
	start := 0
	if text[0] == '-' {
		start = 1
	}
	digits, fraction, point := 0, 0, false
	for i := start; i < len(text); i++ {
		c := text[i]
		if c == '.' && !point && digits > 0 {
			point = true
			continue
		}
		if c < '0' || c > '9' {
			return "", ErrRecordAverageResult
		}
		digits++
		if point {
			fraction++
		}
	}
	if digits == 0 || (point && fraction == 0) {
		return "", ErrRecordAverageResult
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || value.Cmp(new(big.Rat).SetInt64(math.MinInt32)) < 0 || value.Cmp(new(big.Rat).SetInt64(math.MaxInt32)) > 0 {
		return "", ErrRecordAverageResult
	}
	return RecordDecimal(text), nil
}
