package mysqlstore

import (
	"errors"
	"net/netip"
)

// InetColumns is the non-null storage representation of a PostgreSQL inet:
// family (4 or 6), address (VARBINARY(16)), and prefix length. Owner migrations
// must enforce matching byte lengths and prefix ranges with database constraints.
// It intentionally does not implement driver.Valuer: three columns are required.
type InetColumns struct {
	Family     int
	Address    []byte
	PrefixBits int
}

// EncodeInet preserves host bits and IPv4-mapped IPv6 identity. It does not mask
// the prefix or unmap IPv6. Input parsing and API formatting belong to callers.
func EncodeInet(prefix netip.Prefix) (InetColumns, error) {
	if !prefix.IsValid() {
		return InetColumns{}, errors.New("invalid inet prefix")
	}
	family := 6
	if prefix.Addr().Is4() {
		family = 4
	}
	return InetColumns{Family: family, Address: prefix.Addr().AsSlice(), PrefixBits: prefix.Bits()}, nil
}

// Prefix validates all columns before constructing a value. SQL NULL must be
// handled explicitly by owner repositories, never converted to a zero address.
func (c InetColumns) Prefix() (netip.Prefix, error) {
	bits := 0
	switch c.Family {
	case 4:
		bits = 32
	case 6:
		bits = 128
	default:
		return netip.Prefix{}, errors.New("invalid inet family")
	}
	if len(c.Address) != bits/8 || c.PrefixBits < 0 || c.PrefixBits > bits {
		return netip.Prefix{}, errors.New("invalid inet address length or prefix")
	}
	addr, ok := netip.AddrFromSlice(c.Address)
	if !ok {
		return netip.Prefix{}, errors.New("invalid inet address")
	}
	return netip.PrefixFrom(addr, c.PrefixBits), nil
}
