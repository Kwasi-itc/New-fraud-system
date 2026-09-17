package geoip

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type MMDBLookup struct {
	reader *maxminddb.Reader
	locale string
}

type mmdbRecord struct {
	Continent struct {
		Code  string            `maxminddb:"code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
}

func OpenMMDB(path, locale string) (*MMDBLookup, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("geoip MMDB path is empty")
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open geoip MMDB %q: %w", path, err)
	}
	if strings.TrimSpace(reader.Metadata.DatabaseType) == "" {
		_ = reader.Close()
		return nil, fmt.Errorf("open geoip MMDB %q: database type metadata is empty", path)
	}
	return &MMDBLookup{reader: reader, locale: normalizeLocale(locale)}, nil
}

func (l *MMDBLookup) Lookup(ctx context.Context, address netip.Addr) (ports.GeoIPLocation, bool, error) {
	startedAt := time.Now()
	if l == nil || l.reader == nil {
		return ports.GeoIPLocation{}, false, fmt.Errorf("geoip MMDB reader is not configured")
	}
	if err := ctx.Err(); err != nil {
		return ports.GeoIPLocation{}, false, err
	}
	if !address.IsValid() {
		return ports.GeoIPLocation{}, false, fmt.Errorf("invalid IP address")
	}

	normalizedAddress := address.Unmap()
	result := l.reader.Lookup(normalizedAddress)
	if err := result.Err(); err != nil {
		slog.Error("geoip_mmdb_lookup_failed",
			"input_ip", address.String(),
			"normalized_ip", normalizedAddress.String(),
			"database_type", l.reader.Metadata.DatabaseType,
			"duration_micros", time.Since(startedAt).Microseconds(),
			"error", err,
		)
		return ports.GeoIPLocation{}, false, fmt.Errorf("lookup IP address %s: %w", address, err)
	}
	if !result.Found() {
		slog.Debug("geoip_mmdb_lookup_completed",
			"input_ip", address.String(),
			"normalized_ip", normalizedAddress.String(),
			"found", false,
			"database_type", l.reader.Metadata.DatabaseType,
			"database_build_time", l.reader.Metadata.BuildTime(),
			"locale", l.locale,
			"duration_micros", time.Since(startedAt).Microseconds(),
		)
		return ports.GeoIPLocation{Address: normalizedAddress.String()}, false, nil
	}

	var record mmdbRecord
	if err := result.Decode(&record); err != nil {
		slog.Error("geoip_mmdb_decode_failed",
			"input_ip", address.String(),
			"normalized_ip", normalizedAddress.String(),
			"network", result.Prefix().String(),
			"database_type", l.reader.Metadata.DatabaseType,
			"duration_micros", time.Since(startedAt).Microseconds(),
			"error", err,
		)
		return ports.GeoIPLocation{}, false, fmt.Errorf("decode geolocation for %s: %w", address, err)
	}

	location := ports.GeoIPLocation{
		Address:           normalizedAddress.String(),
		Network:           result.Prefix().String(),
		ContinentCode:     record.Continent.Code,
		CountryCode:       record.Country.ISOCode,
		CountryName:       localizedName(record.Country.Names, l.locale),
		DatabaseType:      l.reader.Metadata.DatabaseType,
		DatabaseBuildTime: l.reader.Metadata.BuildTime(),
	}
	if len(record.Subdivisions) > 0 {
		location.RegionCode = record.Subdivisions[0].ISOCode
		location.RegionName = localizedName(record.Subdivisions[0].Names, l.locale)
	}
	slog.Debug("geoip_mmdb_lookup_completed",
		"input_ip", address.String(),
		"normalized_ip", location.Address,
		"found", true,
		"network", location.Network,
		"continent_code", location.ContinentCode,
		"country_code", location.CountryCode,
		"country_name", location.CountryName,
		"region_code", location.RegionCode,
		"region_name", location.RegionName,
		"subdivisions_count", len(record.Subdivisions),
		"database_type", location.DatabaseType,
		"database_build_time", location.DatabaseBuildTime,
		"locale", l.locale,
		"duration_micros", time.Since(startedAt).Microseconds(),
	)
	return location, true, nil
}

func (l *MMDBLookup) Close() error {
	if l == nil || l.reader == nil {
		return nil
	}
	err := l.reader.Close()
	l.reader = nil
	return err
}

func (l *MMDBLookup) DatabaseType() string {
	if l == nil || l.reader == nil {
		return ""
	}
	return l.reader.Metadata.DatabaseType
}

func normalizeLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "en"
	}
	return locale
}

func localizedName(names map[string]string, locale string) string {
	if value := strings.TrimSpace(names[locale]); value != "" {
		return value
	}
	if value := strings.TrimSpace(names["en"]); value != "" {
		return value
	}
	keys := make([]string, 0, len(names))
	for key := range names {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(names[key]); value != "" {
			return value
		}
	}
	return ""
}
