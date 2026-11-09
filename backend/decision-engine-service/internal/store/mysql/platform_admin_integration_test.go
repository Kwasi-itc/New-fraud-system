package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func platformAdminFixture(t *testing.T, f platformFixture, tenant uuid.UUID, rows, bytes int, mutate PlatformAuthorizer) *PlatformAdmin {
	t.Helper()
	s, e := NewPlatformAdmin(f.store(t, tenant, rows, bytes), mutate, serviceIDsFixture{}, serviceClockFixture{time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestMySQLPlatformAdminAuthorityAndTenantIsolation(t *testing.T) {
	f := platformTestDB(t)
	tenant := uuid.New()
	ctx := context.Background()
	s := platformAdminFixture(t, f, tenant, 10, 1024, allowPlatformFixture)
	if _, e := s.CreateTag(ctx, "wallets", "same ", "watch"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateRisk(ctx, "wallets", "same ", "high"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateFlag(ctx, "192.0.2.1 ", "vpn"); e != nil {
		t.Fatal(e)
	}
	other := platformAdminFixture(t, f, uuid.New(), 10, 1024, allowPlatformFixture)
	if v, e := other.Tags(ctx, "wallets", "same "); e != nil || len(v) != 0 {
		t.Fatal(v, e)
	}
	if v, e := other.Flags(ctx, "192.0.2.1 "); e != nil || len(v) != 0 {
		t.Fatal(v, e)
	}
	if _, e := (evaluationPlatform{other.reads}).GetByObject(ctx, other.reads.tenant.String(), "wallets", "same "); !errors.Is(e, ports.ErrRiskSnapshotNotFound) {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		auth PlatformAuthorizer
		want error
	}{
		{"missing", nil, ErrOwnerContractUnavailable},
		{"denied", func(_ context.Context, tx *sql.Tx, id uuid.UUID, write bool) error {
			if tx == nil || id != tenant || !write {
				t.Error("incorrect authorization contract")
			}
			return ErrDecisionAuthorizationDenied
		}, ErrDecisionAuthorizationDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := platformAdminFixture(t, f, tenant, 10, 1024, tc.auth)
			if v, e := a.CreateTag(ctx, "wallets", "bad", "watch"); !errors.Is(e, tc.want) || v.ID != "" {
				t.Fatal(v, e)
			}
			if v, e := a.CreateRisk(ctx, "wallets", "bad", "high"); !errors.Is(e, tc.want) || v.ID != "" {
				t.Fatal(v, e)
			}
			if v, e := a.CreateFlag(ctx, "bad", "vpn"); !errors.Is(e, tc.want) || v.ID != "" {
				t.Fatal(v, e)
			}
			if v, e := a.Tags(ctx, "wallets", "same "); e != nil || len(v) != 1 {
				t.Fatal(v, e)
			}
		})
	}
	// Reads recheck current authority; retaining the admin does not cache grants.
	s.reads.authorize = func(context.Context, *sql.Tx, uuid.UUID, bool) error { return ErrDecisionAuthorizationDenied }
	if v, e := s.Tags(ctx, "wallets", "same "); !errors.Is(e, ErrDecisionAuthorizationDenied) || v != nil {
		t.Fatal(v, e)
	}
}
func TestMySQLPlatformAdminBoundedReadsAndWrites(t *testing.T) {
	f := platformTestDB(t)
	tenant := uuid.New()
	ctx := context.Background()
	s := platformAdminFixture(t, f, tenant, 10, 1024, allowPlatformFixture)
	for i := 0; i < 3; i++ {
		if _, e := s.CreateTag(ctx, "w", "object", strings.Repeat("x", 20)); e != nil {
			t.Fatal(e)
		}
		if _, e := s.CreateFlag(ctx, "ip", strings.Repeat("x", 20)); e != nil {
			t.Fatal(e)
		}
	}
	for _, limit := range []struct {
		name        string
		rows, bytes int
	}{{"rows", 2, 1024}, {"bytes", 10, 30}} {
		t.Run(limit.name, func(t *testing.T) {
			a := platformAdminFixture(t, f, tenant, limit.rows, limit.bytes, allowPlatformFixture)
			if v, e := a.Tags(ctx, "w", "object"); !errors.Is(e, ErrProjectionLimit) || v != nil {
				t.Fatal(v, e)
			}
			if v, e := a.Flags(ctx, "ip"); !errors.Is(e, ErrProjectionLimit) || v != nil {
				t.Fatal(v, e)
			}
		})
	}
	for _, text := range []string{"bad\x00", string([]byte{0xff}), strings.Repeat("x", 1025)} {
		want := ErrAdministrationInput
		if len(text) > 1024 {
			want = ErrProjectionLimit
		}
		if v, e := s.CreateTag(ctx, "w", "invalid", text); !errors.Is(e, want) || v.ID != "" {
			t.Fatal(v, e)
		}
		if v, e := s.CreateRisk(ctx, "w", "invalid", text); !errors.Is(e, want) || v.ID != "" {
			t.Fatal(v, e)
		}
		if v, e := s.CreateFlag(ctx, "invalid", text); !errors.Is(e, want) || v.ID != "" {
			t.Fatal(v, e)
		}
	}
	if v, e := s.Tags(ctx, "w", "invalid"); e != nil || len(v) != 0 {
		t.Fatal(v, e)
	}
}
func TestMySQLPlatformAdminReadbackFailureRollsBack(t *testing.T) {
	f := platformTestDB(t)
	tenant := uuid.New()
	ctx := context.Background()
	s := platformAdminFixture(t, f, tenant, 10, 1024, allowPlatformFixture)
	schema, _ := mysqlstore.Identifier(f.database)
	for _, tc := range []struct {
		table, column string
		create        func() error
	}{
		{"record_tags", "tag", func() error {
			v, e := s.CreateTag(ctx, "w", "rollback", "watch")
			if v.ID != "" {
				t.Error("provisional tag escaped")
			}
			return e
		}},
		{"risk_snapshots", "risk_level", func() error {
			v, e := s.CreateRisk(ctx, "w", "rollback", "high")
			if v.ID != "" {
				t.Error("provisional risk escaped")
			}
			return e
		}},
		{"ip_flags", "flag", func() error {
			v, e := s.CreateFlag(ctx, "rollback", "vpn")
			if v.ID != "" {
				t.Error("provisional flag escaped")
			}
			return e
		}},
	} {
		t.Run(tc.table, func(t *testing.T) {
			trigger := schema + ".platform_readback_fault"
			if _, e := f.db.Exec("CREATE TRIGGER " + trigger + " BEFORE INSERT ON " + schema + ".decision_engine_" + tc.table + " FOR EACH ROW SET NEW." + tc.column + "=REPEAT('x',1025)"); e != nil {
				t.Fatal(e)
			}
			defer func() {
				if _, e := f.db.Exec("DROP TRIGGER " + trigger); e != nil {
					t.Error(e)
				}
			}()
			if e := tc.create(); !errors.Is(e, ErrProjectionLimit) {
				t.Fatal(e)
			}
			var n int
			if e := f.db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_"+tc.table+" WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&n); e != nil || n != 0 {
				t.Fatal(n, e)
			}
		})
	}
}
func TestMySQLPlatformAdminConcurrentWritesSnapshotAndEvaluator(t *testing.T) {
	f := platformTestDB(t)
	tenant := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := platformAdminFixture(t, f, tenant, 50, 1<<20, allowPlatformFixture)
	evaluator := evaluationPlatform{s.reads}
	if e := s.reads.ReadSnapshot(ctx, func(r PlatformRepositories) error {
		before, e := r.RecordTags.HasTag(ctx, tenant.String(), "w", "snapshot", "watch")
		if e != nil || before {
			return fmt.Errorf("initial read %v %w", before, e)
		}
		// This write uses another connection while the read snapshot remains open.
		if _, e = s.CreateTag(ctx, "w", "snapshot", "watch"); e != nil {
			return e
		}
		after, e := r.RecordTags.HasTag(ctx, tenant.String(), "w", "snapshot", "watch")
		if e != nil || after {
			return fmt.Errorf("snapshot changed %v %w", after, e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if found, e := evaluator.HasTag(ctx, tenant.String(), "w", "snapshot", "watch"); e != nil || !found {
		t.Fatal(found, e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.CreateFlag(ctx, "concurrent", fmt.Sprintf("flag-%d", i))
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if v, e := s.Flags(ctx, "concurrent"); e != nil || len(v) != 8 {
		t.Fatal(v, e)
	}
	if found, e := evaluator.HasFlag(ctx, tenant.String(), "concurrent", "flag-7"); e != nil || !found {
		t.Fatal(found, e)
	}
	risk, e := s.CreateRisk(ctx, "w", "snapshot ", "high ")
	if e != nil {
		t.Fatal(e)
	}
	v, e := evaluator.GetByObject(ctx, tenant.String(), "w", "snapshot ")
	if e != nil || v.ID != risk.ID || v.RiskLevel != "high " {
		t.Fatal(v, e)
	}
	if _, e := evaluator.GetByObject(ctx, tenant.String(), "w", "snapshot"); !errors.Is(e, ports.ErrRiskSnapshotNotFound) {
		t.Fatal(e)
	}
}
