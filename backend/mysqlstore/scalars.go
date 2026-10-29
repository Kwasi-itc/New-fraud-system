package mysqlstore

import (
	"database/sql/driver"
	"errors"
	"math"
	"strconv"
)

// Bool preserves the application's Boolean domain. MySQL's numeric Boolean
// alias alone does not enforce it; owner tables must also CHECK(value IN (0,1)).
type Bool bool

func (b Bool) Value() (driver.Value, error) { return bool(b), nil }

func (b *Bool) Scan(value any) error {
	if b == nil {
		return errors.New("nil Boolean destination")
	}
	*b = false
	if v, ok := value.(bool); ok {
		*b = Bool(v)
		return nil
	}
	v, err := scalarInteger(value, 8)
	if err != nil || (v != 0 && v != 1) {
		return errors.New("Boolean requires non-null zero or one")
	}
	*b = v == 1
	return nil
}

// Int32 retains PostgreSQL INTEGER's signed range rather than widening it to
// the Go host's int range. SQL NULL is represented separately by NullInt32.
type Int32 int32

func (n Int32) Value() (driver.Value, error) { return int64(n), nil }

func (n *Int32) Scan(value any) error {
	if n == nil {
		return errors.New("nil integer destination")
	}
	*n = 0
	v, err := scalarInteger(value, 32)
	if err != nil {
		return err
	}
	*n = Int32(v)
	return nil
}

func scalarInteger(value any, bits int) (int64, error) {
	var text string
	switch v := value.(type) {
	case int64:
		if bits == 32 && (v < math.MinInt32 || v > math.MaxInt32) {
			return 0, errors.New("integer outside signed storage range")
		}
		return v, nil
	case []byte:
		text = string(v)
	case string:
		text = v
	default:
		return 0, errors.New("integer requires non-null signed integral storage")
	}
	v, err := strconv.ParseInt(text, 10, bits)
	if err != nil {
		// Do not embed customer values in storage errors.
		return 0, errors.New("invalid or out-of-range signed integer")
	}
	return v, nil
}

// Float64 is a finite DOUBLE, not DECIMAL. This codec deliberately does not
// change fraud-rule arithmetic or silently turn invalid values into no-match.
type Float64 float64

func (n Float64) Value() (driver.Value, error) {
	v := float64(n)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, errors.New("DOUBLE must be finite")
	}
	return v, nil
}

func (n *Float64) Scan(value any) error {
	if n == nil {
		return errors.New("nil DOUBLE destination")
	}
	*n = 0
	var v float64
	var err error
	switch raw := value.(type) {
	case float64:
		v = raw
	case []byte:
		v, err = strconv.ParseFloat(string(raw), 64)
	case string:
		v, err = strconv.ParseFloat(raw, 64)
	default:
		return errors.New("DOUBLE requires non-null floating-point storage")
	}
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return errors.New("invalid or non-finite DOUBLE")
	}
	*n = Float64(v)
	return nil
}

type NullBool struct {
	Bool  Bool
	Valid bool
}

func (n NullBool) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Bool.Value()
}

func (n *NullBool) Scan(value any) error {
	if n == nil {
		return errors.New("nil nullable Boolean destination")
	}
	*n = NullBool{}
	if value == nil {
		return nil
	}
	if err := n.Bool.Scan(value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

type NullInt32 struct {
	Int32 Int32
	Valid bool
}

func (n NullInt32) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Int32.Value()
}

func (n *NullInt32) Scan(value any) error {
	if n == nil {
		return errors.New("nil nullable integer destination")
	}
	*n = NullInt32{}
	if value == nil {
		return nil
	}
	if err := n.Int32.Scan(value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

type NullFloat64 struct {
	Float64 Float64
	Valid   bool
}

func (n NullFloat64) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Float64.Value()
}

func (n *NullFloat64) Scan(value any) error {
	if n == nil {
		return errors.New("nil nullable DOUBLE destination")
	}
	*n = NullFloat64{}
	if value == nil {
		return nil
	}
	if err := n.Float64.Scan(value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}
