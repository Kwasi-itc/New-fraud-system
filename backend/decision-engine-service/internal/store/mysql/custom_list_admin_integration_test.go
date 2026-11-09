package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func listAdminFixture(t *testing.T) (platformFixture, *CustomListAdmin, uuid.UUID) {
	t.Helper()
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true)
	tenant := uuid.New()
	installIdentityOwner(t, f)
	session := seedIdentity(t, f, tenant, "platform_builder")
	authorize, e := SessionAuthorizer(f.database, session)
	if e != nil {
		t.Fatal(e)
	}
	owner, e := NewCustomListStore(f.db, f.database, tenant, 500, 2<<20, authorize)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewCustomListAdmin(owner, serviceIDsFixture{}, serviceClockFixture{time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	return f, s, tenant
}
func adminList(name string) platform.CustomList {
	return platform.CustomList{Name: name, Description: "fixture", Kind: "generic_text"}
}

type repeatedImportID struct {
	n     int
	first uuid.UUID
}

func (g *repeatedImportID) New() uuid.UUID {
	g.n++
	if g.n == 106 {
		return g.first
	}
	id := uuid.New()
	if g.n == 1 {
		g.first = id
	}
	return id
}

type blockingImportID struct {
	once             sync.Once
	reached, release chan struct{}
}

func (g *blockingImportID) New() uuid.UUID {
	g.once.Do(func() { close(g.reached); <-g.release })
	return uuid.New()
}

func TestMySQLCustomListAdminBulkRollbackBoundsAndAuthority(t *testing.T) {
	f, s, tenant := listAdminFixture(t)
	ctx := context.Background()
	list, e := s.Save(ctx, uuid.Nil, adminList("Import"))
	if e != nil {
		t.Fatal(e)
	}
	id := uuid.MustParse(list.ID)
	broken, e := NewCustomListAdmin(s.owner, &repeatedImportID{}, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	values := make([]string, 120)
	for i := range values {
		values[i] = fmt.Sprintf("value-%03d", i)
	}
	if n, e := broken.Import(ctx, id, values); e == nil || n != 0 {
		t.Fatal("partial import returned", n, e)
	}
	if v, e := s.Entries(ctx, id); e != nil || len(v) != 0 {
		t.Fatal("earlier batch committed", v, e)
	}
	if n, e := s.Import(ctx, id, []string{" A ", "A", "a", "\t", "B "}); e != nil || n != 3 {
		t.Fatal("trim/deduplicate", n, e)
	}
	if n, e := s.Import(ctx, id, []string{"A"}); e != nil || n != 1 {
		t.Fatal("existing-value semantics changed", n, e)
	}
	rows, e := s.Entries(ctx, id)
	if e != nil || len(rows) != 4 {
		t.Fatal(rows, e)
	}
	smallOwner, e := NewCustomListStore(f.db, f.database, tenant, 2, 2<<20, s.owner.configuration.authorize)
	if e != nil {
		t.Fatal(e)
	}
	small, e := NewCustomListAdmin(smallOwner, serviceIDsFixture{}, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	if n, e := small.Import(ctx, id, []string{"x", "y", "z"}); !errors.Is(e, ErrProjectionLimit) || n != 0 {
		t.Fatal("row budget", n, e)
	}
	if _, e := small.Entries(ctx, id); !errors.Is(e, ErrProjectionLimit) {
		t.Fatal("silent list truncation", e)
	}
	tinyOwner, e := NewCustomListStore(f.db, f.database, tenant, 500, 150, s.owner.configuration.authorize)
	if e != nil {
		t.Fatal(e)
	}
	tiny, e := NewCustomListAdmin(tinyOwner, serviceIDsFixture{}, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	if n, e := tiny.Import(ctx, id, []string{"one", "two"}); !errors.Is(e, ErrProjectionLimit) || n != 0 {
		t.Fatal("byte budget", n, e)
	}
	var actions []AuthoringAction
	restricted, e := NewCustomListStore(f.db, f.database, tenant, 500, 2<<20, func(_ context.Context, _ *sql.Tx, t uuid.UUID, a AuthoringAction) error {
		actions = append(actions, a)
		if t != tenant || a != AuthoringCustomListRead {
			return ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	readOnly, e := NewCustomListAdmin(restricted, serviceIDsFixture{}, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := readOnly.Get(ctx, id); e != nil {
		t.Fatal(e)
	}
	if n, e := readOnly.Import(ctx, id, nil); !errors.Is(e, ErrDecisionAuthorizationDenied) || n != 0 {
		t.Fatal("empty import bypassed permission", n, e)
	}
	if !reflect.DeepEqual(actions, []AuthoringAction{AuthoringCustomListRead, AuthoringCustomListWrite}) {
		t.Fatal(actions)
	}
	otherOwner, e := NewCustomListStore(f.db, f.database, uuid.New(), 500, 2<<20, allowAuthoringFixture)
	if e != nil {
		t.Fatal(e)
	}
	other, e := NewCustomListAdmin(otherOwner, serviceIDsFixture{}, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := other.Get(ctx, id); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("foreign list read", e)
	}
	if n, e := other.Import(ctx, id, []string{"forged"}); !errors.Is(e, sql.ErrNoRows) || n != 0 {
		t.Fatal("foreign import", n, e)
	}
	if v, e := s.Entries(ctx, id); e != nil || len(v) != 4 {
		t.Fatal("failed operations wrote rows", v, e)
	}
}

func TestMySQLCustomListAdminImportRenameConcurrencyAndIndependentLists(t *testing.T) {
	f, s, tenant := listAdminFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(f.database)
	list, e := s.Save(ctx, uuid.Nil, adminList("Before"))
	if e != nil {
		t.Fatal(e)
	}
	id := uuid.MustParse(list.ID)
	other, e := s.Save(ctx, uuid.Nil, adminList("Independent"))
	if e != nil {
		t.Fatal(e)
	}
	gate := &blockingImportID{reached: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(gate.release) })
	importing, e := NewCustomListAdmin(s.owner, gate, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	imported := make(chan error, 1)
	go func() { _, e := importing.Import(ctx, id, []string{"A", "B"}); imported <- e }()
	select {
	case <-gate.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	renamed := make(chan error, 1)
	go func() { _, e := s.Save(ctx, id, adminList("After")); renamed <- e }()
	for {
		var waiting int
		e = f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='decision_engine_custom_lists'", f.database).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting > 0 {
			break
		}
		select {
		case e := <-renamed:
			t.Fatal("rename did not wait for import", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	independent, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if _, e := s.Save(independent, uuid.MustParse(other.ID), adminList("Independent updated")); e != nil {
		t.Fatal("unrelated list blocked", e)
	}
	release.Do(func() { close(gate.release) })
	if e = <-imported; e != nil {
		t.Fatal(e)
	}
	if e = <-renamed; e != nil {
		t.Fatal(e)
	}
	entries, e := s.Entries(ctx, id)
	if e != nil || len(entries) != 2 {
		t.Fatal(entries, e)
	}
	for _, v := range entries {
		if v.ListName != "After" {
			t.Fatal(v)
		}
	}
	var stale int
	if e = f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_custom_list_entries WHERE tenant_id=? AND list_id=? AND CAST(list_name AS BINARY)<>CAST('After' AS BINARY)", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&stale); e != nil || stale != 0 {
		t.Fatal("stale denormalized name", stale, e)
	}
	if e = s.owner.ReadSnapshot(ctx, func(r CustomListRepository) error {
		found, e := r.Contains(ctx, tenant.String(), "After", "A")
		if e == nil && !found {
			t.Fatal("evaluation cannot see renamed import")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}

func TestMySQLCustomListAdminImportPlans(t *testing.T) {
	_, s, tenant := listAdminFixture(t)
	ctx := context.Background()
	list, e := s.Save(ctx, uuid.Nil, adminList("Plan fixture"))
	if e != nil {
		t.Fatal(e)
	}
	id := uuid.MustParse(list.ID)
	values := make([]string, 256)
	for i := range values {
		values[i] = fmt.Sprintf("value-%03d", i)
	}
	if n, e := s.Import(ctx, id, values); e != nil || n != 256 {
		t.Fatal(n, e)
	}
	if e = s.owner.ReadSnapshot(ctx, func(r CustomListRepository) error {
		var plan string
		if e := r.tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+r.entrySQL()+" WHERE e.tenant_id=? AND e.list_id=? ORDER BY e.created_at DESC,e.id DESC LIMIT 51", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&plan); e != nil {
			return e
		}
		t.Logf("256-row linked-entry query plan; not production acceptance: %s", plan)
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
