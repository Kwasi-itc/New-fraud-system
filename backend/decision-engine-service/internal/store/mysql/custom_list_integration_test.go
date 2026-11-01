package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestMySQLScreeningIgnoreWordsBoundsAndScope(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true)
	ctx := context.Background()
	tenant := uuid.New()
	s, err := NewCustomListStore(f.db, f.database, tenant, 2, 4096, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	list := platform.CustomList{ID: uuid.NewString(), TenantID: tenant.String(), Name: "Ignore", Kind: "generic_text", CreatedAt: at, UpdatedAt: at}
	if err := s.Mutate(ctx, func(r CustomListRepository) error { _, err := r.CreateList(ctx, list); return err }); err != nil {
		t.Fatal(err)
	}
	read := func() ([]string, error) {
		var out []string
		err := s.ReadSnapshot(ctx, func(r CustomListRepository) error {
			var err error
			out, err = r.ScreeningIgnoreWords(ctx, tenant.String(), list.ID)
			return err
		})
		return out, err
	}
	if out, err := read(); err != nil || len(out) != 0 {
		t.Fatal("empty list", out, err)
	}
	if err := s.ReadSnapshot(ctx, func(r CustomListRepository) error {
		if _, err := r.ScreeningIgnoreWords(ctx, uuid.NewString(), list.ID); !errors.Is(err, ErrTenantScope) {
			t.Fatal("scope", err)
		}
		if _, err := r.ScreeningIgnoreWords(ctx, tenant.String(), uuid.NewString()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("missing list", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entry := platform.CustomListEntry{ID: uuid.NewString(), TenantID: tenant.String(), ListID: &list.ID, ListName: list.Name, Value: "Case Preserved ", CreatedAt: at}
	if err := s.Mutate(ctx, func(r CustomListRepository) error { _, err := r.Create(ctx, entry); return err }); err != nil {
		t.Fatal(err)
	}
	if out, err := read(); err != nil || len(out) != 1 || out[0] != entry.Value {
		t.Fatal("value changed", out, err)
	}
	entry.Value = strings.Repeat("x", 1001)
	if err := s.Mutate(ctx, func(r CustomListRepository) error { _, err := r.UpdateEntry(ctx, entry); return err }); err != nil {
		t.Fatal(err)
	}
	if out, err := read(); !errors.Is(err, ErrProjectionLimit) || out != nil {
		t.Fatal("oversize value", out, err)
	}
	entry.Value = "first"
	if err := s.Mutate(ctx, func(r CustomListRepository) error {
		if _, err := r.UpdateEntry(ctx, entry); err != nil {
			return err
		}
		for _, value := range []string{"second", "third"} {
			v := entry
			v.ID = uuid.NewString()
			v.Value = value
			if _, err := r.Create(ctx, v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if out, err := read(); !errors.Is(err, ErrProjectionLimit) || out != nil {
		t.Fatal("row limit", out, err)
	}
}

func TestMySQLCustomListFoldExactValuesAndRenameAtomicity(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true)
	ctx := context.Background()
	tenant := uuid.New()
	s, err := NewCustomListStore(f.db, f.database, tenant, 100, 1<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	list := platform.CustomList{ID: uuid.NewString(), TenantID: tenant.String(), Name: "RiskNames", Description: "example", Kind: "generic_text", CreatedAt: at, UpdatedAt: at}
	entry := platform.CustomListEntry{ID: uuid.NewString(), TenantID: tenant.String(), ListID: &list.ID, ListName: list.Name, Value: "Owner ", CreatedAt: at}
	if err := s.Mutate(ctx, func(r CustomListRepository) error {
		if _, err := r.CreateList(ctx, list); err != nil {
			return err
		}
		_, err := r.Create(ctx, entry)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	duplicate := list
	duplicate.ID = uuid.NewString()
	duplicate.Name = "risknames"
	if err := s.Mutate(ctx, func(r CustomListRepository) error { _, err := r.CreateList(ctx, duplicate); return err }); err == nil {
		t.Fatal("folded name not unique")
	}
	trailing := list
	trailing.ID = uuid.NewString()
	trailing.Name += " "
	if err := s.Mutate(ctx, func(r CustomListRepository) error { _, err := r.CreateList(ctx, trailing); return err }); err != nil {
		t.Fatal("name trailing space lost", err)
	}
	if err := s.ReadSnapshot(ctx, func(r CustomListRepository) error {
		yes, err := r.Contains(ctx, tenant.String(), "RISKnames", entry.Value)
		if err != nil {
			return err
		}
		if !yes {
			t.Fatal("folded lookup")
		}
		yes, err = r.Contains(ctx, tenant.String(), list.Name, "owner ")
		if err != nil {
			return err
		}
		if yes {
			t.Fatal("entry value folded")
		}
		yes, err = r.Contains(ctx, tenant.String(), list.Name, "Owner")
		if err != nil {
			return err
		}
		if yes {
			t.Fatal("entry trailing space lost")
		}
		_, err = r.ListByName(ctx, uuid.NewString(), list.Name)
		if !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback rename")
	changed := list
	changed.Name = "Renamed"
	if err := s.Mutate(ctx, func(r CustomListRepository) error {
		if _, err := r.UpdateList(ctx, changed); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r CustomListRepository) error {
		items, err := r.ListEntriesByListID(ctx, tenant.String(), list.ID)
		if err == nil && (len(items) != 1 || items[0].ListName != list.Name) {
			t.Fatal("rename rollback")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(ctx, func(r CustomListRepository) error { _, err := r.UpdateList(ctx, changed); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r CustomListRepository) error {
		items, err := r.ListByName(ctx, tenant.String(), changed.Name)
		if err == nil && (len(items) != 1 || items[0].ListName != changed.Name) {
			t.Fatal("rename result")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
