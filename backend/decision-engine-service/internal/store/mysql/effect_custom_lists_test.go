package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestEffectCustomListsRequireExplicitAuthority(t *testing.T) {
	tx := &sql.Tx{} // Identity-only fixture; no SQL is executed in this pure test.
	if _, err := (EffectRepositories{}).CustomLists(); !errors.Is(err, ErrDecisionAuthorizationUnavailable) {
		t.Fatal("unconfigured list access succeeded", err)
	}
	if _, err := (*EffectStore)(nil).WithCustomLists(nil, false); err == nil {
		t.Fatal("missing store/authority accepted")
	}
	store := &EffectStore{configuration: &PlatformStore{tenant: uuid.New()}}
	denied := errors.New("distinct list permission denied")
	var got AuthoringAction
	authorize := func(context.Context, *sql.Tx, uuid.UUID, AuthoringAction) error { return denied }
	if _, err := store.WithCustomLists(nil, true); err == nil {
		t.Fatal("generic effect permission replaced list permission")
	}
	readOnly, err := store.WithCustomLists(func(_ context.Context, _ *sql.Tx, _ uuid.UUID, action AuthoringAction) error {
		got = action
		return nil
	}, false)
	if err != nil || store.lists != nil {
		t.Fatal("mutated source configuration", err)
	}
	if _, err := readOnly.authorizedCustomLists(context.Background(), nil, true); !errors.Is(err, ErrDecisionAuthorizationUnavailable) {
		t.Fatal("missing transaction accepted", err)
	}
	repo, err := readOnly.authorizedCustomLists(context.Background(), tx, true)
	if err != nil || got != AuthoringCustomListRead || repo.write {
		t.Fatal("decision mutation conferred list mutation permission", got, err)
	}
	write, err := store.WithCustomLists(func(_ context.Context, _ *sql.Tx, _ uuid.UUID, action AuthoringAction) error {
		got = action
		return nil
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	repo, err = write.authorizedCustomLists(context.Background(), tx, true)
	if err != nil || got != AuthoringCustomListWrite || !repo.write {
		t.Fatal("explicit list mutation permission missing", got, err)
	}
	repo, err = write.authorizedCustomLists(context.Background(), tx, false)
	if err != nil || got != AuthoringCustomListRead || repo.write {
		t.Fatal("snapshot got mutation permission", got, err)
	}
	write, err = store.WithCustomLists(authorize, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.authorizedCustomLists(context.Background(), tx, true); !errors.Is(err, denied) {
		t.Fatal("denial bypassed", err)
	}
}
