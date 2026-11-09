package mysql

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func historyParityPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testsupport.PostgresDatabaseURL(t)
	dir, e := filepath.Abs("../../migrations/metadata")
	if e != nil {
		t.Fatal(e)
	}
	m, e := migrate.New("file://"+dir, dsn)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Up(); e != nil {
		t.Fatal(e)
	}
	if a, b := m.Close(); a != nil || b != nil {
		t.Fatal(a, b)
	}
	pg, e := pgxpool.New(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pg.Close)
	return pg
}

// Real SQL on both engines, compared with independently filtered golden rows.
// Search parity is scoped to these strings and the fixture PG locale, not every
// possible Unicode/locale combination or application-scale load acceptance.
func TestHistoryPostgresMySQLGoldenFiltersPaginationAndSnoozes(t *testing.T) {
	ctx := context.Background()
	pg := historyParityPostgres(t)
	f := scenarioFoundationDB(t, true, true, true)
	tenant, parent := uuid.New(), uuid.New()
	if e := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Wallet Risk"); e != nil {
		t.Fatal(e)
	}
	owner := authoringStore(t, f, tenant, 500, 2<<20)
	live := createAuthoringDraft(t, owner, tenant, parent)
	target, e := NewDecisionStore(f.db, f.database, tenant, 500, 2<<20, allowPlatformFixture)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 123456000, time.UTC)
	tm := postgres.NewTransactionManager(pg)
	if e := tm.Run(ctx, func(s ports.MutationStore) error {
		if _, e := s.Scenarios().Create(ctx, scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Wallet Risk", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}); e != nil {
			return e
		}
		_, e := s.Iterations().Create(ctx, live)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	values := make([]decision.Decision, 48)
	names := []string{"", "Wallet", "Wallet ", "wallet", "Café", "Cafe", `A_%\`, "Other"}
	outcomes := []decision.Outcome{decision.OutcomeApprove, decision.OutcomeReview, decision.OutcomeBlockAndReview, decision.OutcomeDecline}
	for i := range values {
		v := decisionInput(tenant, parent, live.ID)
		v.ObjectID = names[i%len(names)]
		v.Score = i - 16
		v.CreatedAt = at.Add(time.Duration(i/3) * time.Microsecond)
		v.Outcome = outcomes[i%4]
		v.Triggered = i%2 == 0
		if i%5 == 0 {
			v.ObjectType = "events"
		}
		values[i] = v
	}
	if e := tm.Run(ctx, func(s ports.MutationStore) error {
		for _, v := range values {
			if _, e := s.Decisions().Create(ctx, v); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := target.Mutate(ctx, func(s DecisionRepositories) error {
		for _, v := range values {
			if _, e := s.Decisions.Create(ctx, v); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	summaries := func(v []decision.Decision) string {
		out := make([]decision.Decision, len(v))
		copy(out, v)
		for i := range out {
			out[i].RequestBody = nil
			out[i].CreatedAt = out[i].CreatedAt.UTC()
		}
		raw, e := json.Marshal(out)
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	source := postgres.NewDecisionRepository(pg)
	low, high := 0, 15
	negative := -8
	var combined decision.Decision
	for _, v := range values {
		if v.ObjectID == "Wallet" && v.ObjectType == "wallets" && v.Score >= low && v.Score <= high {
			combined = v
			break
		}
	}
	if combined.ID == "" {
		t.Fatal("combined filter fixture must have a positive match")
	}
	cases := []struct {
		name   string
		filter ports.DecisionListFilter
		match  func(decision.Decision) bool
	}{
		{"all", ports.DecisionListFilter{}, func(decision.Decision) bool { return true }},
		{"exact object", ports.DecisionListFilter{ObjectID: "Wallet "}, func(v decision.Decision) bool { return v.ObjectID == "Wallet " }},
		{"object type", ports.DecisionListFilter{ObjectType: "events"}, func(v decision.Decision) bool { return v.ObjectType == "events" }},
		{"outcome", ports.DecisionListFilter{Outcome: "review"}, func(v decision.Decision) bool { return v.Outcome == decision.OutcomeReview }},
		{"score range", ports.DecisionListFilter{MinScore: &low, MaxScore: &high}, func(v decision.Decision) bool { return v.Score >= low && v.Score <= high }},
		{"prefix", ports.DecisionListFilter{ObjectIDPrefix: "Wal"}, func(v decision.Decision) bool { return strings.HasPrefix(v.ObjectID, "Wal") }},
		{"escaped wildcard prefix", ports.DecisionListFilter{ObjectIDPrefix: `A\_\%`}, func(v decision.Decision) bool { return strings.HasPrefix(v.ObjectID, "A_%") }},
		{"scenario search", ports.DecisionListFilter{Search: "wALLET rISK"}, func(decision.Decision) bool { return true }},
		{"accent search", ports.DecisionListFilter{Search: "CAFÉ"}, func(v decision.Decision) bool { return v.ObjectID == "Café" }},
		{"accent sensitive search", ports.DecisionListFilter{Search: "cafe"}, func(v decision.Decision) bool { return v.ObjectID == "Cafe" }},
		{"combined", ports.DecisionListFilter{ScenarioID: parent.String(), ObjectType: "wallets", Outcome: "approve", MinScore: &low}, func(v decision.Decision) bool {
			return v.ObjectType == "wallets" && v.Outcome == decision.OutcomeApprove && v.Score >= 0
		}},
		{"decision ID", ports.DecisionListFilter{DecisionID: values[0].ID}, func(v decision.Decision) bool { return v.ID == values[0].ID }},
		{"missing scenario", ports.DecisionListFilter{ScenarioID: uuid.NewString()}, func(decision.Decision) bool { return false }},
		{"reversed scores", ports.DecisionListFilter{MinScore: &high, MaxScore: &low}, func(decision.Decision) bool { return false }},
		{"minimum only", ports.DecisionListFilter{MinScore: &negative}, func(v decision.Decision) bool { return v.Score >= negative }},
		{"maximum only", ports.DecisionListFilter{MaxScore: &negative}, func(v decision.Decision) bool { return v.Score <= negative }},
		{"exact zero score", ports.DecisionListFilter{MinScore: &low, MaxScore: &low}, func(v decision.Decision) bool { return v.Score == 0 }},
		{"UUID search", ports.DecisionListFilter{Search: strings.ToUpper(values[0].ID)}, func(v decision.Decision) bool { return v.ID == values[0].ID }},
		{"object type search", ports.DecisionListFilter{Search: "EvEnTs"}, func(v decision.Decision) bool { return v.ObjectType == "events" }},
		{"literal wildcard search", ports.DecisionListFilter{Search: `A\_\%`}, func(v decision.Decision) bool { return strings.HasPrefix(v.ObjectID, "A_%") }},
		{"all filters positive", ports.DecisionListFilter{DecisionID: combined.ID, ScenarioID: parent.String(), ObjectID: combined.ObjectID, ObjectType: combined.ObjectType, Outcome: string(combined.Outcome), ObjectIDPrefix: "Wal", Search: "risk", MinScore: &low, MaxScore: &high}, func(v decision.Decision) bool { return v.ID == combined.ID }},
		{"all filters mismatch", ports.DecisionListFilter{DecisionID: combined.ID, ScenarioID: parent.String(), ObjectID: combined.ObjectID, ObjectType: combined.ObjectType, Outcome: string(combined.Outcome), ObjectIDPrefix: "Wal", Search: "missing", MinScore: &low, MaxScore: &high}, func(decision.Decision) bool { return false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := []decision.Decision{}
			for _, v := range values {
				if tc.match(v) {
					golden = append(golden, v)
				}
			}
			check := func(r ports.DecisionRepository) error {
				count, e := r.CountFiltered(ctx, tenant.String(), tc.filter)
				if e != nil {
					return e
				}
				if count != len(golden) {
					t.Fatalf("count %d want %d", count, len(golden))
				}
				all, e := r.ListFiltered(ctx, tenant.String(), tc.filter)
				if e != nil {
					return e
				}
				if summaries(all) != summaries(golden) {
					t.Fatalf("full list mismatch got %s want %s", summaries(all), summaries(golden))
				}
				for _, pageSize := range []int{1, 5, len(values)} {
					var cursor *ports.DecisionListCursor
					seen := []decision.Decision{}
					for page := 0; page <= len(golden)/pageSize; page++ {
						got, more, e := r.ListFilteredCursor(ctx, tenant.String(), tc.filter, pageSize, cursor)
						if e != nil {
							return e
						}
						if more != (len(seen)+len(got) < len(golden)) {
							t.Fatalf("wrong hasMore %v", more)
						}
						for _, v := range got {
							if len(v.RequestBody) != 0 {
								t.Fatal("payload leaked into summary")
							}
						}
						offset, offsetMore, e := r.ListFilteredPage(ctx, tenant.String(), tc.filter, pageSize, len(seen))
						if e != nil {
							return e
						}
						if offsetMore != more || summaries(offset) != summaries(got) {
							t.Fatal("offset/cursor mismatch")
						}
						seen = append(seen, got...)
						if len(got) > 0 {
							last := got[len(got)-1]
							cursor = &ports.DecisionListCursor{CreatedAt: last.CreatedAt, ID: last.ID}
						}
						if !more {
							break
						}
						if len(got) == 0 {
							t.Fatal("empty nonterminal page")
						}
					}
					if summaries(seen) != summaries(golden) {
						t.Fatal("pagination skipped or duplicated rows")
					}
					if cursor != nil {
						tail, more, err := r.ListFilteredCursor(ctx, tenant.String(), tc.filter, pageSize, cursor)
						if err != nil {
							return err
						}
						if len(tail) != 0 || more {
							t.Fatal("exhausted cursor returned more rows")
						}
					}
					past, more, err := r.ListFilteredPage(ctx, tenant.String(), tc.filter, pageSize, len(golden)+1)
					if err != nil {
						return err
					}
					if len(past) != 0 || more {
						t.Fatal("offset past end returned rows")
					}
				}
				return nil
			}
			if e := check(source); e != nil {
				t.Fatal("PG", e)
			}
			if e := target.ReadHistory(ctx, func(r HistoryRepositories) error { return check(r.Decisions) }); e != nil {
				t.Fatal("MySQL", e)
			}
		})
	}
	t.Run("exact empty whitespace case and foreign tenant", func(t *testing.T) {
		for _, object := range names {
			golden := []decision.Decision{}
			for _, v := range values {
				if v.ObjectType == "wallets" && v.ObjectID == object {
					golden = append(golden, v)
				}
			}
			check := func(r ports.DecisionRepository) error {
				got, e := r.ListByObject(ctx, tenant.String(), "wallets", object)
				if e != nil {
					return e
				}
				n, e := r.CountByObject(ctx, tenant.String(), "wallets", object)
				if e != nil {
					return e
				}
				if n != len(golden) || summaries(got) != summaries(golden) {
					t.Fatalf("exact object %q mismatch", object)
				}
				return nil
			}
			if e := check(source); e != nil {
				t.Fatal(e)
			}
			if e := target.ReadHistory(ctx, func(r HistoryRepositories) error { return check(r.Decisions) }); e != nil {
				t.Fatal(e)
			}
		}
		other := uuid.New()
		otherTarget, e := NewDecisionStore(f.db, f.database, other, 500, 2<<20, allowPlatformFixture)
		if e != nil {
			t.Fatal(e)
		}
		n, e := source.CountByTenant(ctx, other.String())
		if e != nil || n != 0 {
			t.Fatal(n, e)
		}
		if e := otherTarget.ReadHistory(ctx, func(r HistoryRepositories) error {
			n, e := r.Decisions.CountByTenant(ctx, other.String())
			if n != 0 {
				t.Fatal("tenant leaked")
			}
			return e
		}); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("cursor remains stable across inserts and absent anchor", func(t *testing.T) {
		first, more, e := source.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 5, nil)
		if e != nil || !more || len(first) != 5 {
			t.Fatal(first, more, e)
		}
		if e := target.ReadHistory(ctx, func(r HistoryRepositories) error {
			got, more, e := r.Decisions.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 5, nil)
			if e == nil && (!more || summaries(got) != summaries(first)) {
				t.Fatal("first page mismatch")
			}
			return e
		}); e != nil {
			t.Fatal(e)
		}
		older, newer := decisionInput(tenant, parent, live.ID), decisionInput(tenant, parent, live.ID)
		older.CreatedAt, newer.CreatedAt = at.Add(-time.Hour), at.Add(time.Hour)
		for _, v := range []decision.Decision{older, newer} {
			if e := tm.Run(ctx, func(s ports.MutationStore) error { _, e := s.Decisions().Create(ctx, v); return e }); e != nil {
				t.Fatal(e)
			}
			if e := target.Mutate(ctx, func(r DecisionRepositories) error { _, e := r.Decisions.Create(ctx, v); return e }); e != nil {
				t.Fatal(e)
			}
		}
		want := append(append([]decision.Decision{}, values[5:]...), older)
		last := first[len(first)-1]
		cursor := &ports.DecisionListCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		seen := []decision.Decision{}
		for page := 0; page < 20; page++ {
			left, more, e := source.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 5, cursor)
			if e != nil {
				t.Fatal(e)
			}
			if e := target.ReadHistory(ctx, func(r HistoryRepositories) error {
				right, rightMore, e := r.Decisions.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 5, cursor)
				if e == nil && (rightMore != more || summaries(right) != summaries(left)) {
					t.Fatal("changing-history cursor mismatch")
				}
				return e
			}); e != nil {
				t.Fatal(e)
			}
			seen = append(seen, left...)
			if !more {
				break
			}
			if len(left) == 0 {
				t.Fatal("empty nonterminal cursor")
			}
			last = left[len(left)-1]
			cursor = &ports.DecisionListCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
		if summaries(seen) != summaries(want) {
			t.Fatal("cursor skipped/duplicated history or admitted a newer row")
		}
		// Cursor values define an exclusive ordering boundary, without requiring
		// a persisted anchor. Evidence deletion itself is intentionally forbidden.
		absent := &ports.DecisionListCursor{CreatedAt: at.Add(-30 * time.Minute), ID: uuid.NewString()}
		left, more, e := source.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 5, absent)
		if e != nil || more || summaries(left) != summaries([]decision.Decision{older}) {
			t.Fatal("source absent cursor", left, more, e)
		}
		if e := target.ReadHistory(ctx, func(r HistoryRepositories) error {
			right, more, e := r.Decisions.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 5, absent)
			if e == nil && (more || summaries(right) != summaries(left)) {
				t.Fatal("target absent cursor", right, more)
			}
			return e
		}); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("count and page retain one snapshot across concurrent commits", func(t *testing.T) {
		readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		tx, err := pg.BeginTx(readCtx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		pgReader := postgres.NewDecisionRepository(tx)
		filter := ports.DecisionListFilter{}
		before, err := pgReader.CountFiltered(readCtx, tenant.String(), filter)
		if err != nil {
			t.Fatal(err)
		}
		page, _, err := pgReader.ListFilteredCursor(readCtx, tenant.String(), filter, 5, nil)
		if err != nil {
			t.Fatal(err)
		}
		inserted := decisionInput(tenant, parent, live.ID)
		inserted.CreatedAt = at.Add(2 * time.Hour)
		if err := target.ReadHistory(readCtx, func(r HistoryRepositories) error {
			count, err := r.Decisions.CountFiltered(readCtx, tenant.String(), filter)
			if err != nil {
				return err
			}
			if count != before {
				t.Fatal("initial snapshot counts differ", count, before)
			}
			// Separate writer transactions must finish while both read snapshots
			// remain open; no read lock may serialize unrelated inserts.
			if err := tm.Run(readCtx, func(s ports.MutationStore) error { _, err := s.Decisions().Create(readCtx, inserted); return err }); err != nil {
				return err
			}
			if err := target.Mutate(readCtx, func(w DecisionRepositories) error { _, err := w.Decisions.Create(readCtx, inserted); return err }); err != nil {
				return err
			}
			for _, repo := range []ports.DecisionRepository{pgReader, r.Decisions} {
				n, err := repo.CountFiltered(readCtx, tenant.String(), filter)
				if err != nil {
					return err
				}
				got, _, err := repo.ListFilteredCursor(readCtx, tenant.String(), filter, 5, nil)
				if err != nil {
					return err
				}
				if n != before || summaries(got) != summaries(page) {
					t.Fatal("count/page snapshot changed after writer commit", n, before)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(readCtx); err != nil {
			t.Fatal(err)
		}
		if n, err := source.CountFiltered(readCtx, tenant.String(), filter); err != nil || n != before+1 {
			t.Fatal("source fresh snapshot missed insert", n, err)
		}
		if err := target.ReadHistory(readCtx, func(r HistoryRepositories) error {
			n, err := r.Decisions.CountFiltered(readCtx, tenant.String(), filter)
			if err == nil && n != before+1 {
				t.Fatal("target fresh snapshot missed insert", n)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("snooze exclusive expiry and exact objects", func(t *testing.T) {
		sn := make([]snooze.RuleSnooze, 3)
		for i := range sn {
			sn[i] = snooze.RuleSnooze{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), SnoozeGroupID: uuid.NewString(), ObjectType: "wallets", ObjectID: "Wallet ", CreatedAt: at, ExpiresAt: at.Add(time.Duration(i+1) * time.Hour)}
		}
		if e := tm.Run(ctx, func(s ports.MutationStore) error {
			for _, v := range sn {
				if _, e := s.RuleSnoozes().Create(ctx, v); e != nil {
					return e
				}
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		if e := target.Mutate(ctx, func(r DecisionRepositories) error {
			for _, v := range sn {
				if _, e := r.RuleSnoozes.Create(ctx, v); e != nil {
					return e
				}
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		for _, object := range []string{"Wallet ", "Wallet", "wallet "} {
			for i := 0; i <= 3; i++ {
				now := at.Add(time.Duration(i) * time.Hour)
				want := 3 - i
				if object != "Wallet " {
					want = 0
				}
				left, e := postgres.NewRuleSnoozeRepository(pg).ListActive(ctx, tenant.String(), parent.String(), "wallets", object, now)
				if e != nil {
					t.Fatal(e)
				}
				if e := target.ReadHistory(ctx, func(r HistoryRepositories) error {
					right, e := r.RuleSnoozes.ListActive(ctx, tenant.String(), parent.String(), "wallets", object, now)
					if e != nil {
						return e
					}
					ids := func(v []snooze.RuleSnooze) []string {
						out := make([]string, len(v))
						for i := range v {
							out[i] = v[i].ID
						}
						return out
					}
					if len(left) != want || !reflect.DeepEqual(ids(left), ids(right)) {
						t.Fatal("expiry/scope mismatch", object, now, left, right)
					}
					return nil
				}); e != nil {
					t.Fatal(e)
				}
			}
		}
	})
}
