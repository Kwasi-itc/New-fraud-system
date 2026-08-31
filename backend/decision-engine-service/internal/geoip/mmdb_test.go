package geoip

import (
	"context"
	"net/netip"
	"os"
	"testing"
)

func TestLocalizedNameUsesLocaleThenEnglish(t *testing.T) {
	t.Parallel()

	names := map[string]string{"en": "Ghana", "fr": "Ghana (fr)"}
	if got := localizedName(names, "fr"); got != "Ghana (fr)" {
		t.Fatalf("localizedName() = %q, want Ghana (fr)", got)
	}
	if got := localizedName(names, "de"); got != "Ghana" {
		t.Fatalf("localizedName() fallback = %q, want Ghana", got)
	}
}

func TestConfiguredMMDBCanResolvePublicIP(t *testing.T) {
	path := os.Getenv("GEOIP_TEST_MMDB_PATH")
	if path == "" {
		t.Skip("GEOIP_TEST_MMDB_PATH is not set")
	}

	lookup, err := OpenMMDB(path, "en")
	if err != nil {
		t.Fatalf("OpenMMDB() error = %v", err)
	}
	t.Cleanup(func() { _ = lookup.Close() })

	addressText := os.Getenv("GEOIP_TEST_ADDRESS")
	if addressText == "" {
		addressText = "8.8.8.8"
	}
	address, err := netip.ParseAddr(addressText)
	if err != nil {
		t.Fatalf("parse GEOIP_TEST_ADDRESS: %v", err)
	}
	location, found, err := lookup.Lookup(context.Background(), address)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if !found {
		t.Fatal("Lookup() found = false, want true")
	}
	if location.CountryCode == "" || location.CountryName == "" || location.Network == "" {
		t.Fatalf("Lookup() returned incomplete location: %#v", location)
	}
	if location.DatabaseType == "" || location.DatabaseBuildTime.IsZero() {
		t.Fatalf("Lookup() returned incomplete database metadata: %#v", location)
	}
	t.Logf("GeoIP lookup for %s: %#v", address, location)
}
