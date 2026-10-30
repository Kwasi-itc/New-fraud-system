package mysqljobs

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func identityFixture() Identity {
	return Identity{Version: 1, Kind: ports.JobAsync, TenantID: uuid.MustParse("01234567-89ab-4cde-8fab-0123456789ab"), ResourceID: uuid.MustParse("12345678-9abc-4def-8abc-123456789abc"), DomainAttempt: 1}
}

func TestAllDecisionKindsUseExplicitTargetIdentity(t *testing.T) {
	count := 0
	for _, source := range workers.LegacyCatalog() {
		if source.Owner != "decision_engine" {
			continue
		}
		count++
		v := identityFixture()
		v.Kind = ports.JobKind(source.Kind)
		raw, err := Encode(v)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(raw)
		if err != nil || decoded != v {
			t.Fatal("round trip", decoded, err)
		}
		entry, err := decoded.Contract()
		if err != nil || entry != source || entry.Unique != workers.ArgsRetained {
			t.Fatal("source catalog contract", entry, err)
		}
	}
	if count != 7 {
		t.Fatal("decision catalog changed; audit all handlers", count)
	}
}

func TestDecisionDeliveryReservationSeparatesDomainAndTransportAttempts(t *testing.T) {
	initial := identityFixture()
	transportRetry := initial
	nextDomain := initial
	nextDomain.DomainAttempt++
	redrive := initial
	redrive.RedriveGeneration++
	callback := initial
	callback.Kind = ports.JobCallback
	otherTenant := initial
	otherTenant.TenantID = uuid.New()
	otherResource := initial
	otherResource.ResourceID = uuid.New()
	// The exact tuple, not queue labels/JSON hashes, is the reservation key.
	initialKey, err := initial.Key()
	if err != nil {
		t.Fatal(err)
	}
	retryKey, err := transportRetry.Key()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[ReservationKey]bool{initialKey: true}
	if !seen[retryKey] {
		t.Fatal("transport retry lost stable identity")
	}
	for _, v := range []Identity{nextDomain, redrive, callback, otherTenant, otherResource} {
		key, err := v.Key()
		if err != nil {
			t.Fatal(err)
		}
		if seen[key] {
			t.Fatal("legitimate owner identity was deduplicated")
		}
		if err := v.Validate(); err != nil {
			t.Fatal(err)
		}
		seen[key] = true
	}
}

func TestDecisionDeliveryIdentityRejectsAmbiguousOrUnversionedPayloads(t *testing.T) {
	raw, err := Encode(identityFixture())
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"payload_version", "kind", "tenant_id", "resource_id", "domain_attempt", "redrive_generation"} {
		for _, value := range []any{nil, "missing"} {
			copy := map[string]any{}
			for k, v := range base {
				copy[k] = v
			}
			if value == "missing" {
				delete(copy, field)
			} else {
				copy[field] = value
			}
			raw, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(raw); !errors.Is(err, ErrIdentity) {
				t.Fatal("missing/NULL field accepted", field, err)
			}
		}
	}
	for _, test := range []struct {
		field string
		value any
	}{
		{"payload_version", 0}, {"payload_version", 2}, {"kind", "index_job_execute"}, {"kind", "arbitrary"}, {"tenant_id", uuid.Nil.String()}, {"tenant_id", strings.ToUpper(identityFixture().TenantID.String())}, {"resource_id", "urn:uuid:" + identityFixture().ResourceID.String()}, {"domain_attempt", 0}, {"domain_attempt", -1}, {"domain_attempt", 1.5}, {"domain_attempt", int64(math.MaxInt32) + 1}, {"redrive_generation", -1}, {"redrive_generation", 1.5}, {"redrive_generation", int64(math.MaxInt32) + 1}, {"unknown", true},
	} {
		copy := map[string]any{}
		for k, v := range base {
			copy[k] = v
		}
		copy[test.field] = test.value
		raw, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(raw); !errors.Is(err, ErrIdentity) {
			t.Fatal("invalid payload accepted", test.field, test.value, err)
		}
	}
	for _, raw := range []string{string(raw) + ` {}`, strings.TrimSuffix(string(raw), "}") + `,"domain_attempt":1}`, `[]`, `null`, `{"execution_id":"12345678-9abc-4def-8abc-123456789abc"}`, strings.Repeat(" ", 4097)} {
		if _, err := Decode([]byte(raw)); !errors.Is(err, ErrIdentity) {
			t.Fatal("ambiguous/legacy payload accepted", err)
		}
	}
	limit := identityFixture()
	limit.DomainAttempt = math.MaxInt32
	limit.RedriveGeneration = math.MaxInt32
	if _, err := Encode(limit); err != nil {
		t.Fatal("valid signed upper bounds", err)
	}
}
