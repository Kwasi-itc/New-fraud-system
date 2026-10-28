package mysqlstore

import (
	"database/sql"
	"errors"
	"net/netip"
)

// NullInet represents one optional address across three SQL columns. All three
// must be NULL or all present. It is not a single-column Scanner or Valuer.
type NullInet struct {
	Prefix netip.Prefix
	Valid  bool
}

// Values returns family, binary address and prefix bits in that order. Bind
// each element separately; owner migrations must enforce the tuple invariant.
func (n NullInet) Values() ([3]any, error) {
	if !n.Valid {
		return [3]any{}, nil
	}
	c, err := EncodeInet(n.Prefix)
	if err != nil {
		return [3]any{}, err
	}
	return [3]any{c.Family, c.Address, c.PrefixBits}, nil
}

// ScanColumns consumes the three raw values after a successful row Scan.
// State is cleared on NULL and error. Callers must handle both Scan errors and
// this validation error. Non-null address data must be binary, never text.
func (n *NullInet) ScanColumns(family, address, prefixBits any) error {
	if n == nil {
		return errors.New("nil nullable inet destination")
	}
	*n = NullInet{}
	if family == nil && address == nil && prefixBits == nil {
		return nil
	}
	if family == nil || address == nil || prefixBits == nil {
		return errors.New("partially null inet columns")
	}
	var f, p sql.NullInt64
	if err := f.Scan(family); err != nil {
		return errors.New("invalid inet family column")
	}
	if err := p.Scan(prefixBits); err != nil {
		return errors.New("invalid inet prefix column")
	}
	b, ok := address.([]byte)
	if !ok || (f.Int64 != 4 && f.Int64 != 6) || p.Int64 < 0 || p.Int64 > 128 {
		return errors.New("invalid inet columns")
	}
	value, err := (InetColumns{Family: int(f.Int64), Address: b, PrefixBits: int(p.Int64)}).Prefix()
	if err != nil {
		return err
	}
	n.Prefix, n.Valid = value, true
	return nil
}
