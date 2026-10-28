package mysqlstore

import (
	"database/sql/driver"
	"errors"
)

// UUID uses standard 16-byte UUID order. Text identifiers (including screening
// match IDs) must not be stored through this codec. SQL NULL is rejected.
type UUID [16]byte

func (u UUID) Value() (driver.Value, error) {
	b := make([]byte, 16)
	copy(b, u[:])
	return b, nil
}

func (u *UUID) Scan(value any) error {
	if u == nil {
		return errors.New("nil UUID destination")
	}
	b, ok := value.([]byte)
	if !ok || len(b) != 16 {
		return errors.New("UUID requires exactly 16 non-null binary bytes")
	}
	copy(u[:], b)
	return nil
}
