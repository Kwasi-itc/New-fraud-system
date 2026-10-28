package mysqlstore

import (
	"database/sql/driver"
	"errors"
)

// NullUUID distinguishes SQL NULL from the all-zero UUID. Valid values use the
// same standard binary byte order as UUID, without a textual fallback.
type NullUUID struct {
	UUID  UUID
	Valid bool
}

func (n NullUUID) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.UUID.Value()
}

// Scan clears previous state even on error, so reuse cannot expose a stale ID.
func (n *NullUUID) Scan(value any) error {
	if n == nil {
		return errors.New("nil nullable UUID destination")
	}
	*n = NullUUID{}
	if value == nil {
		return nil
	}
	if err := n.UUID.Scan(value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

// NullDateTime represents an optional UTC DATETIME(6). NULL is distinct from
// a zero timestamp; valid values retain DateTime's range and precision checks.
type NullDateTime struct {
	DateTime DateTime
	Valid    bool
}

func (n NullDateTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.DateTime.Value()
}

func (n *NullDateTime) Scan(value any) error {
	if n == nil {
		return errors.New("nil nullable datetime destination")
	}
	*n = NullDateTime{}
	if value == nil {
		return nil
	}
	if err := n.DateTime.Scan(value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}
