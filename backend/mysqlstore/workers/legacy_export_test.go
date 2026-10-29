package workers

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestLegacyExportAllKindsAndTenantBindings(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	zero := int64(0)
	for _, contract := range LegacyCatalog() {
		fields := map[string]string{}
		scope := ExportScope{}
		if !contract.Global {
			scope = ExportScope{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
			fields[contract.ResourceField] = scope.ResourceID
		}
		if contract.TenantInPayload {
			fields["tenant_id"] = scope.TenantID
		}
		payload, _ := json.Marshal(fields)
		source := LegacyExport{Kind: contract.Kind, Payload: payload, State: "available", MaxAttempts: 3, ErrorCount: &zero, CreatedAt: now, AvailableAt: now}
		checked, err := CheckLegacyExport(source, scope, now)
		if err != nil || checked.Identity.Kind != contract.Kind || checked.Identity.TenantID != scope.TenantID || string(checked.Source.Payload) != string(payload) {
			t.Fatalf("kind=%s: %+v %v", contract.Kind, checked, err)
		}
		if !contract.Global {
			if _, err := CheckLegacyExport(source, ExportScope{}, now); err == nil {
				t.Fatal("tenant was inferred without authoritative binding")
			}
			wrong := scope
			wrong.ResourceID = "33333333-3333-4333-8333-333333333333"
			if _, err := CheckLegacyExport(source, wrong, now); err == nil {
				t.Fatal("different resource binding accepted")
			}
			if contract.TenantInPayload {
				wrong = scope
				wrong.TenantID = "33333333-3333-4333-8333-333333333333"
				if _, err := CheckLegacyExport(source, wrong, now); err == nil {
					t.Fatal("cross-tenant export accepted")
				}
			}
		} else if _, err := CheckLegacyExport(source, ExportScope{TenantID: "11111111-1111-4111-8111-111111111111"}, now); err == nil {
			t.Fatal("global job assigned a fake tenant")
		}
	}
}

func TestLegacyExportRejectsUnreconciledWork(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	zero := int64(0)
	valid := LegacyExport{Kind: "case_maintenance", Payload: []byte(`{}`), State: "available", MaxAttempts: 5, ErrorCount: &zero, CreatedAt: now, AvailableAt: now}
	for _, change := range []func(*LegacyExport){func(s *LegacyExport) { s.State = "unknown" }, func(s *LegacyExport) { s.ErrorCount = nil }, func(s *LegacyExport) { s.Attempt = -1 }, func(s *LegacyExport) { s.MaxAttempts = 0 }, func(s *LegacyExport) { s.PayloadVersion = 99 }, func(s *LegacyExport) { s.CreatedAt = now.Add(time.Second) }, func(s *LegacyExport) { s.CreatedAt = now.Add(time.Nanosecond) }, func(s *LegacyExport) { s.State = "completed" }, func(s *LegacyExport) { s.FinalizedAt = &now }} {
		source := valid
		change(&source)
		if _, err := CheckLegacyExport(source, ExportScope{}, now); err == nil {
			t.Fatal("invalid work export accepted")
		}
	}
	running := valid
	running.State = "running"
	if _, err := CheckLegacyExport(running, ExportScope{}, now); !errors.Is(err, ErrDrainRequired) {
		t.Fatal("active legacy lease accepted for replay")
	}
	terminal := valid
	terminal.State = "cancelled"
	terminal.FinalizedAt = &now
	checked, err := CheckLegacyExport(terminal, ExportScope{}, now)
	if err != nil {
		t.Fatal(err)
	}
	terminal.Payload[0] = '['
	*terminal.ErrorCount = 4
	if string(checked.Source.Payload) != `{}` || *checked.Source.ErrorCount != 0 {
		t.Fatal("checked export retained mutable source aliases")
	}
}
