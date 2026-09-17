package ports

import (
	"context"
	"net/netip"
	"time"
)

// GeoIPLocation is a provider-neutral view of an IP geolocation record.
// Region refers to the first state/province-level administrative subdivision
// returned by the configured database.
type GeoIPLocation struct {
	Address           string    `json:"address"`
	Network           string    `json:"network,omitempty"`
	ContinentCode     string    `json:"continent_code,omitempty"`
	CountryCode       string    `json:"country_code,omitempty"`
	CountryName       string    `json:"country_name,omitempty"`
	RegionCode        string    `json:"region_code,omitempty"`
	RegionName        string    `json:"region_name,omitempty"`
	DatabaseType      string    `json:"database_type,omitempty"`
	DatabaseBuildTime time.Time `json:"database_build_time,omitempty"`
}

// GeoIPLookup keeps the rule evaluator independent of a particular MMDB
// implementation or data vendor.
type GeoIPLookup interface {
	Lookup(ctx context.Context, address netip.Addr) (GeoIPLocation, bool, error)
}
