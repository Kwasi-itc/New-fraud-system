package mysqlstore

import (
	"database/sql/driver"
	"errors"
	"time"
)

// DateTime represents a non-null DATETIME(6) instant in UTC. Callers must make
// precision loss explicit before binding: silently rounding a version/lease time
// can change its equality or expiry semantics. Open-ended values use SQL NULL
// through a separate nullable column contract, never a sentinel date.
type DateTime struct{ time.Time }

func (d DateTime) Value() (driver.Value, error) {
	t, err := dateTime(d.Time)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (d *DateTime) Scan(value any) error {
	if d == nil {
		return errors.New("nil datetime destination")
	}
	t, ok := value.(time.Time)
	if !ok {
		return errors.New("datetime requires a non-null parsed time")
	}
	t, err := dateTime(t)
	if err != nil {
		return err
	}
	d.Time = t
	return nil
}

func dateTime(t time.Time) (time.Time, error) {
	t = t.UTC()
	if t.Year() < 1000 || t.Year() > 9999 {
		return time.Time{}, errors.New("datetime outside MySQL DATETIME range")
	}
	if t.Nanosecond()%1000 != 0 {
		return time.Time{}, errors.New("datetime precision exceeds microseconds")
	}
	return t, nil
}
