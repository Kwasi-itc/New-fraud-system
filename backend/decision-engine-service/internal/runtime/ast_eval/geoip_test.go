package ast_eval

import (
	"context"
	"net/netip"
	"sync"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type geoIPLookupStub struct {
	mu    sync.Mutex
	calls int
}

func (s *geoIPLookupStub) Lookup(_ context.Context, address netip.Addr) (ports.GeoIPLocation, bool, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return ports.GeoIPLocation{
		Address:       address.String(),
		CountryCode:   "GH",
		CountryName:   "Ghana",
		RegionCode:    "AA",
		RegionName:    "Greater Accra",
		ContinentCode: "AF",
	}, true, nil
}

func (s *geoIPLookupStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestGeoIPFunctionsSharePerEvaluationLookup(t *testing.T) {
	t.Parallel()

	lookup := &geoIPLookupStub{}
	runtime := Runtime{
		Fields:           map[string]any{"client_ip": "8.8.8.8"},
		GeoIPLookup:      lookup,
		GeoIPResultCache: NewGeoIPResultCache(),
	}
	source := domainast.Node{Function: "Payload", Children: []domainast.Node{{Constant: "client_ip"}}}

	country, err := EvaluateNode(context.Background(), domainast.Node{Function: "IPCountry", Children: []domainast.Node{source}}, runtime)
	if err != nil {
		t.Fatalf("IPCountry evaluation error = %v", err)
	}
	region, err := EvaluateNode(context.Background(), domainast.Node{Function: "IPRegion", Children: []domainast.Node{source}}, runtime)
	if err != nil {
		t.Fatalf("IPRegion evaluation error = %v", err)
	}
	if country != "Ghana" || region != "Greater Accra" {
		t.Fatalf("geolocation values = (%v, %v), want (Ghana, Greater Accra)", country, region)
	}
	if got := lookup.callCount(); got != 1 {
		t.Fatalf("lookup calls = %d, want 1", got)
	}
}

func TestGeoIPCountrySupportsNotInListRule(t *testing.T) {
	t.Parallel()

	lookup := &geoIPLookupStub{}
	source := domainast.Node{Function: "Payload", Children: []domainast.Node{{Constant: "client_ip"}}}
	formula := domainast.Node{
		Function: "IsNotInList",
		Children: []domainast.Node{
			{Function: "IPCountry", Children: []domainast.Node{source}},
			{Function: "List", Children: []domainast.Node{{Constant: "Togo"}, {Constant: "Ghana"}}},
		},
	}
	result, err := EvaluateNode(context.Background(), formula, Runtime{
		Fields:           map[string]any{"client_ip": "8.8.8.8"},
		GeoIPLookup:      lookup,
		GeoIPResultCache: NewGeoIPResultCache(),
	})
	if err != nil {
		t.Fatalf("IsNotInList geolocation evaluation error = %v", err)
	}
	if result != false {
		t.Fatalf("IsNotInList geolocation result = %v, want false", result)
	}
}

func TestGeoIPFunctionValidationRequiresIPAddressField(t *testing.T) {
	t.Parallel()

	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"transactions": {
			Name: "transactions",
			Fields: map[string]ports.TenantModelField{
				"client_ip": {Name: "client_ip", Type: "ip_address"},
				"email":     {Name: "email", Type: "string"},
			},
		},
	}}
	valid := domainast.Node{
		Function: "IPCountry",
		Children: []domainast.Node{{Function: "Payload", Children: []domainast.Node{{Constant: "client_ip"}}}},
	}
	valueType, errs := ValidateNode(valid, model, "transactions")
	if len(errs) > 0 || valueType != domainast.ValueTypeString {
		t.Fatalf("ValidateNode(valid) = (%s, %v), want (string, nil)", valueType, errs)
	}

	invalid := valid
	invalid.Children = []domainast.Node{{Function: "Payload", Children: []domainast.Node{{Constant: "email"}}}}
	if _, errs := ValidateNode(invalid, model, "transactions"); len(errs) == 0 {
		t.Fatal("ValidateNode(string field) errors = nil, want ip_address type error")
	}
}
