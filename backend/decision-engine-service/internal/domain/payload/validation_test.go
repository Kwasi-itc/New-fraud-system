package payload

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func model() ports.TenantModel {
	return ports.TenantModel{RevisionID: "rev-1", RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{
		"wallets": {Name: "wallets", Fields: map[string]ports.TenantModelField{
			"user_id": {Name: "user_id", Type: "string"},
			"count":   {Name: "count", Type: "int"},
			"note":    {Name: "note", Type: "string", Nullable: true},
			"old":     {Name: "old", Type: "string", Archived: true},
		}},
	}}
}

func TestCompletenessIdentityAndNull(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]any
		codes  []string
	}{
		{"complete", map[string]any{"user_id": "customer", "count": json.Number("3")}, nil},
		{"missing", map[string]any{"user_id": "customer"}, []string{"missing_required"}},
		{"null", map[string]any{"user_id": "customer", "count": nil}, []string{"null_not_allowed"}},
		{"invalid", map[string]any{"user_id": "customer", "count": "bad"}, []string{"invalid_type"}},
		{"identity", map[string]any{"user_id": "customer", "count": 1, "object_id": "other"}, []string{"identity_mismatch"}},
		{"unknown null", map[string]any{"user_id": "customer", "count": 1, "bad": nil}, []string{"unknown_field"}},
		{"archived", map[string]any{"user_id": "customer", "count": 1, "old": "x"}, []string{"unknown_field"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := NewValidator().Validate("tenant", "wallets", "wallet", tc.values, model(), false)
			if tc.codes == nil {
				if err != nil {
					t.Fatal(err)
				}
				if out["object_id"] != "wallet" || out["count"] != int64(3) {
					t.Fatalf("output: %#v", out)
				}
				if note, ok := out["note"]; !ok || note != nil {
					t.Fatal("nullable omission not materialized")
				}
				if _, ok := tc.values["object_id"]; ok {
					t.Fatal("input mutated")
				}
				return
			}
			var failure *Error
			if !errors.As(err, &failure) {
				t.Fatalf("error: %v", err)
			}
			codes := make([]string, len(failure.Issues))
			for i, issue := range failure.Issues {
				codes[i] = issue.Code
			}
			if !reflect.DeepEqual(codes, tc.codes) {
				t.Fatalf("codes: %#v", codes)
			}
		})
	}
}

func TestNormalization(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		input any
		want  any
		valid bool
	}{
		{"bool", " TRUE ", true, true}, {"bool", "false", false, true}, {"bool", 1, nil, false},
		{"int", json.Number("9223372036854775807"), int64(math.MaxInt64), true},
		{"int", " -9223372036854775808 ", int64(math.MinInt64), true},
		{"int", "9223372036854775808", nil, false}, {"int", 1.5, nil, false}, {"int", float64(9007199254740992), nil, false},
		{"float", " 1.25 ", 1.25, true}, {"float", "NaN", nil, false}, {"float", "Inf", nil, false}, {"float", "1e999", nil, false},
		{"timestamp", "2026-10-06T10:00:00.123+01:00", time.Date(2026, 10, 6, 9, 0, 0, 123000000, time.UTC), true},
		{"timestamp", "yesterday", nil, false},
		{"ip_address", " 2001:db8::1 ", "2001:db8::1", true}, {"ip_address", "bad", nil, false},
		{"string", "  note  ", "  note  ", true}, {"string", 4, nil, false},
	} {
		t.Run(fmt.Sprintf("%s/%v", tc.kind, tc.input), func(t *testing.T) {
			out, valid := normalize(tc.kind, tc.input)
			if valid != tc.valid || valid && !reflect.DeepEqual(out, tc.want) {
				t.Fatalf("got %#v,%v; want %#v,%v", out, valid, tc.want, tc.valid)
			}
		})
	}
}

func TestEnumsRevisionsAndTenantIsolation(t *testing.T) {
	v := NewValidator()
	m := model()
	table := m.Tables["wallets"]
	table.Fields["count"] = ports.TenantModelField{Type: "int", IsEnum: true, EnumValues: []string{"1"}}
	if _, err := v.Validate("a", "wallets", "wallet", map[string]any{"user_id": "u", "count": "1"}, m, false); err != nil {
		t.Fatal(err)
	}
	table.Fields["count"] = ports.TenantModelField{Type: "int", IsEnum: true, EnumValues: []string{"2"}}
	if _, err := v.Validate("b", "wallets", "wallet", map[string]any{"user_id": "u", "count": "1"}, m, false); err == nil {
		t.Fatal("tenant enum cache leaked")
	}
	m.RevisionID = "rev-2"
	if _, err := v.Validate("a", "wallets", "wallet", map[string]any{"user_id": "u", "count": "1"}, m, false); err == nil {
		t.Fatal("old revision accepted")
	}
}

func TestManagedMetadataAndStoredFailures(t *testing.T) {
	values := map[string]any{"user_id": "u", "count": 1, "id": "row-id", "updated_at": "2026-10-06T00:00:00Z", "valid_until": nil}
	if _, err := NewValidator().Validate("t", "wallets", "w", values, model(), true); err != nil {
		t.Fatal(err)
	}
	values["count"] = nil
	_, err := NewValidator().Validate("t", "wallets", "w", values, model(), true)
	var failure *Error
	if !errors.As(err, &failure) || failure.Category != "stored_record_invalid" {
		t.Fatalf("%v", err)
	}
}

func TestBoundedDeterministicErrorsAndCache(t *testing.T) {
	v := NewValidator()
	v.capacity = 2
	values := map[string]any{"user_id": "u", "count": 1}
	for i := 0; i < 150; i++ {
		values[fmt.Sprintf("unknown-%03d", i)] = "sensitive-value"
	}
	_, err := v.Validate("a", "wallets", "w", values, model(), false)
	var failure *Error
	if !errors.As(err, &failure) || !failure.Truncated || len(failure.Issues) != MaxIssues {
		t.Fatalf("%v", err)
	}
	if failure.Issues[0].Field != "unknown-000" || failure.Issues[99].Field != "unknown-099" {
		t.Fatal("issues not ordered")
	}
	for _, tenant := range []string{"b", "c"} {
		_, _ = v.Validate(tenant, "wallets", "w", values, model(), false)
	}
	if len(v.entries) != 2 {
		t.Fatalf("cache size %d", len(v.entries))
	}
	v.ttl = -time.Second
	_, _ = v.Validate("d", "wallets", "w", values, model(), false)
	_, _ = v.Validate("e", "wallets", "w", values, model(), false)
	if len(v.entries) > 2 {
		t.Fatal("cache bound exceeded")
	}
}

func TestConcurrentValidation(t *testing.T) {
	v := NewValidator()
	m := model()
	var group sync.WaitGroup
	for i := 0; i < 40; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := v.Validate("a", "wallets", "w", map[string]any{"user_id": "u", "count": 1}, m, false); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
}

func BenchmarkValidation(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := model()
			table := m.Tables["wallets"]
			table.Fields = make(map[string]ports.TenantModelField, count)
			values := make(map[string]any, count)
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("f%d", i)
				table.Fields[name] = ports.TenantModelField{Type: "float"}
				values[name] = 100.0
			}
			m.Tables["wallets"] = table
			v := NewValidator()
			_, _ = v.Validate("a", "wallets", "w", values, m, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := v.Validate("a", "wallets", "w", values, m, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
