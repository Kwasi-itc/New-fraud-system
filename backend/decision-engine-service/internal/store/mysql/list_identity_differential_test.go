package mysql

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const parityListID = "48f91642-3d8f-4e09-8c82-c296434839d1"
const parityForeignListID = "48f91642-3d8f-4e09-8c82-c296434839d2"

func TestActualDecisionServiceRealIdentityListParity(t *testing.T) {
	actualDecisionServiceParityMode(t, false, true)
}

func realListGoldenCorpus() []scalarGoldenCase {
	contains := func(name, value string) map[string]any {
		return scalarNamed("in_custom_list", map[string]any{"list": name, "value": value})
	}
	count := func(ref string, n int) map[string]any {
		return scalarCall("eq", scalarCall("list_count", scalarNamed("custom_list_access", map[string]any{"customListId": ref})), scalarConstant(n))
	}
	return []scalarGoldenCase{
		{name: "name_hit", formula: contains("Watch", "Exact Value "), matched: true},
		{name: "case_changed_name_misses", formula: contains("WATCH", "Exact Value "), matched: false},
		{name: "value_is_exact", formula: contains("Watch", "Exact Value"), matched: false},
		{name: "missing_name", formula: contains("missing", "Exact Value "), matched: false},
		{name: "id_access", formula: count(parityListID, 1), matched: true},
		{name: "name_access", formula: count("Watch", 1), matched: true},
		{name: "case_changed_access_misses", formula: count("WATCH", 0), matched: true},
		{name: "foreign_id_hidden", formula: count(parityForeignListID, 0), matched: true},
		{name: "foreign_name_hidden", formula: contains("Foreign", "Secret"), matched: false},
	}
}

func prepareListIdentityParity(t *testing.T, ctx context.Context, pg *pgxpool.Pool, f effectFixture, previous *DecisionUnitOfWorkStore) (AuthoringAuthorizer, PlatformAuthorizer, ports.EvaluationCustomLists, ports.EvaluationCustomLists, *DecisionUnitOfWorkStore) {
	t.Helper()
	installIdentityOwner(t, f.platformFixture)
	session := seedIdentity(t, f.platformFixture, f.tenant, "platform_admin")
	auth, err := SessionAuthorizer(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	platformAuth := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		action := AuthoringRead
		if write {
			action = AuthoringWrite
		}
		return auth(ctx, tx, tenant, action)
	}
	target, err := NewCustomListStore(f.db, f.database, f.tenant, 500, 2<<20, auth)
	if err != nil {
		t.Fatal(err)
	}
	effects, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, platformAuth, nil, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	effects, err = effects.WithCustomLists(auth, false)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := NewDecisionUnitOfWorkStore(effects, previous.executions)
	if err != nil {
		t.Fatal(err)
	}
	// Execute the actual source owner definitions, not permissive test functions.
	for _, path := range []string{"data-model-service/internal/migrations/metadata/000001_init.up.sql", "auth-service/internal/platform/schema.sql", "auth-service/internal/platform/schema_v2.sql"} {
		raw, err := os.ReadFile(filepath.Join("../../../..", filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pg.Exec(ctx, string(raw)); err != nil {
			t.Fatal(path, err)
		}
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pg.Exec(ctx, "INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'parity',$2,'active',$3,$3)", f.tenant, "tenant_"+f.tenant.String(), at); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO identity.users(id,display_name) VALUES($1,'parity')", session.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_admin'])", f.tenant, session.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,decode(repeat('ab',32),'hex'),$2,$3,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')", session.SessionID, f.tenant, session.ActorID); err != nil {
		t.Fatal(err)
	}
	sourceRepo := postgres.NewCustomListRepository(pg)
	for _, spec := range []struct {
		id, name, value string
		tenant          uuid.UUID
	}{{parityListID, "Watch", "Exact Value ", f.tenant}, {parityForeignListID, "Foreign", "Secret", uuid.New()}} {
		list := platform.CustomList{ID: spec.id, TenantID: spec.tenant.String(), Name: spec.name, Kind: "generic_text", CreatedAt: at, UpdatedAt: at}
		entry := platform.CustomListEntry{ID: uuid.NewString(), TenantID: list.TenantID, ListID: &list.ID, ListName: list.Name, Value: spec.value, CreatedAt: at}
		if _, err := sourceRepo.CreateList(ctx, list); err != nil {
			t.Fatal(err)
		}
		if _, err := sourceRepo.Create(ctx, entry); err != nil {
			t.Fatal(err)
		}
		current := target
		if spec.tenant != f.tenant {
			foreign := seedIdentity(t, f.platformFixture, spec.tenant, "platform_admin")
			foreignAuth, _ := SessionAuthorizer(f.database, foreign)
			current, err = NewCustomListStore(f.db, f.database, spec.tenant, 500, 2<<20, foreignAuth)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := current.Mutate(ctx, func(r CustomListRepository) error {
			if _, err := r.CreateList(ctx, list); err != nil {
				return err
			}
			_, err := r.Create(ctx, entry)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	return auth, platformAuth, authorizedSourceLists{pg, session}, evaluationLists{target}, unit
}

// Source list reads use the existing source auth-owner contract on the same
// snapshot as the actual PostgreSQL list repository. This fixture adapter does
// not manufacture grants; source administrator policy is intentionally tested.
type authorizedSourceLists struct {
	db      *pgxpool.Pool
	session TrustedDecisionSession
}

func sourceListRead[T any](ctx context.Context, r authorizedSourceLists, tenant string, fn func(postgres.CustomListRepository) (T, error)) (out T, err error) {
	if tenant != r.session.TenantID.String() {
		return out, ErrDecisionAuthorizationDenied
	}
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.Background())
	var allowed bool
	if err := tx.QueryRow(ctx, "SELECT identity.authorize_data_model_v1($1,$2,$3,$4,false,false)", r.session.ActorID.String(), r.session.TenantID, r.session.SessionID, r.session.MembershipVersion).Scan(&allowed); err != nil {
		return out, err
	}
	if !allowed {
		return out, ErrDecisionAuthorizationDenied
	}
	out, err = fn(postgres.NewCustomListRepository(tx))
	if err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}
func (r authorizedSourceLists) GetListByID(c context.Context, t, id string) (platform.CustomList, error) {
	return sourceListRead(c, r, t, func(p postgres.CustomListRepository) (platform.CustomList, error) { return p.GetListByID(c, t, id) })
}
func (r authorizedSourceLists) ListEntriesByListID(c context.Context, t, id string) ([]platform.CustomListEntry, error) {
	return sourceListRead(c, r, t, func(p postgres.CustomListRepository) ([]platform.CustomListEntry, error) {
		return p.ListEntriesByListID(c, t, id)
	})
}
func (r authorizedSourceLists) ListByName(c context.Context, t, name string) ([]platform.CustomListEntry, error) {
	return sourceListRead(c, r, t, func(p postgres.CustomListRepository) ([]platform.CustomListEntry, error) {
		return p.ListByName(c, t, name)
	})
}
func (r authorizedSourceLists) Contains(c context.Context, t, name, value string) (bool, error) {
	return sourceListRead(c, r, t, func(p postgres.CustomListRepository) (bool, error) { return p.Contains(c, t, name, value) })
}
func (r authorizedSourceLists) ScreeningIgnoreWords(c context.Context, t, id string) ([]string, error) {
	return sourceListRead(c, r, t, func(p postgres.CustomListRepository) ([]string, error) { return p.ScreeningIgnoreWords(c, t, id) })
}
