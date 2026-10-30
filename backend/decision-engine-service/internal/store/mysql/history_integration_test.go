package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/google/uuid"
)

func TestMySQLSnoozeExpiryTenantAndRollback(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Snoozed scenario"); err != nil {
		t.Fatal(err)
	}
	s, err := NewDecisionStore(f.db, f.database, tenant, 100, 1<<20, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	v := snooze.RuleSnooze{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), SnoozeGroupID: uuid.NewString(), ObjectType: "wallets", ObjectID: "Object ", CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	if err := s.Mutate(ctx, func(r DecisionRepositories) error { _, err := r.RuleSnoozes.Create(ctx, v); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		active, err := r.RuleSnoozes.ListActive(ctx, tenant.String(), parent.String(), v.ObjectType, v.ObjectID, at)
		if err != nil {
			return err
		}
		if len(active) != 1 {
			t.Fatal(active)
		}
		active, err = r.RuleSnoozes.ListActive(ctx, tenant.String(), parent.String(), v.ObjectType, v.ObjectID, v.ExpiresAt)
		if err != nil {
			return err
		}
		if len(active) != 0 {
			t.Fatal("expiry is exclusive")
		}
		active, err = r.RuleSnoozes.ListActive(ctx, tenant.String(), parent.String(), v.ObjectType, "Object", at)
		if err != nil {
			return err
		}
		if len(active) != 0 {
			t.Fatal("trailing space lost")
		}
		_, err = r.RuleSnoozes.ListActive(ctx, uuid.NewString(), parent.String(), v.ObjectType, v.ObjectID, at)
		if !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback snooze")
	v.ID = uuid.NewString()
	if err := s.Mutate(ctx, func(r DecisionRepositories) error {
		if _, err := r.RuleSnoozes.Create(ctx, v); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		active, err := r.RuleSnoozes.ListActive(ctx, tenant.String(), parent.String(), v.ObjectType, v.ObjectID, at)
		if err == nil && len(active) != 1 {
			t.Fatal("rollback failed")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
