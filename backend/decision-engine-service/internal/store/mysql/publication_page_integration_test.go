package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPublicationPageScopeBoundsAndCursor(t *testing.T) {
	f := scenarioFoundationDB(t, true)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Paged history"); err != nil {
		t.Fatal(err)
	}
	owner := authoringStore(t, f, tenant, 2, 2<<20)
	iteration := createAuthoringDraft(t, owner, tenant, parent)
	schema, _ := mysqlstore.Identifier(f.database)
	at := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 5; i++ {
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_scenario_publications(id,tenant_id,scenario_id,iteration_id,action,created_at) VALUES(?,?,?,?,'publish',?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), mysqlstore.UUID(parent), mysqlstore.UUID(uuid.MustParse(iteration.ID)), mysqlstore.DateTime{Time: at}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(s *AuthoringStore, f PublicationPageFilter) (out PublicationPage, err error) {
		err = s.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
			var err error
			out, err = r.Publications.Page(ctx, tenant.String(), parent.String(), f)
			return err
		})
		return out, err
	}
	first, err := read(owner, PublicationPageFilter{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.Next == nil {
		t.Fatal(first, err)
	}
	seen := map[string]bool{}
	page := first
	for {
		for _, v := range page.Items {
			if seen[v.ID] {
				t.Fatal("duplicate event")
			}
			seen[v.ID] = true
		}
		if page.Next == nil {
			break
		}
		page, err = read(owner, PublicationPageFilter{Limit: 2, After: page.Next})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 5 {
		t.Fatal("lost history", seen)
	}
	for _, mutate := range []func(*PublicationPageCursor){
		func(c *PublicationPageCursor) { c.Version = 2 }, func(c *PublicationPageCursor) { c.TenantID = uuid.New() }, func(c *PublicationPageCursor) { c.ScenarioID = uuid.New() }, func(c *PublicationPageCursor) { c.ID = uuid.Nil }, func(c *PublicationPageCursor) { c.CreatedAt = time.Time{} }, func(c *PublicationPageCursor) { c.CreatedAt = c.CreatedAt.Add(time.Nanosecond) },
	} {
		c := *first.Next
		mutate(&c)
		if _, err := read(owner, PublicationPageFilter{Limit: 2, After: &c}); !errors.Is(err, ErrExecutionPage) {
			t.Fatal("invalid cursor accepted", c, err)
		}
	}
	if _, err := read(owner, PublicationPageFilter{Limit: 3}); !errors.Is(err, ErrExecutionPage) {
		t.Fatal("row budget ignored", err)
	}
	tiny := authoringStore(t, f, tenant, 2, 128)
	if _, err := read(tiny, PublicationPageFilter{Limit: 1}); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("byte budget ignored", err)
	}
}
