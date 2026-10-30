package mysql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Synthetic plan evidence, not a production-volume latency/load acceptance test.
func TestMySQLDecisionHistoryRepresentativeIndexedPlans(t *testing.T) {
	f, s, tenant, parent, live, _, _ := decisionFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	// Use multiple scenarios so the scenario path is selective. An all-one-
	// scenario fixture legitimately lets the optimizer prefer the tenant index.
	type binding struct {
		parent    uuid.UUID
		iteration string
	}
	bindings := []binding{{parent, live.ID}}
	authoring := authoringStore(t, f, tenant, 500, 2<<20)
	for range 19 {
		p := uuid.New()
		if err := scenarioInsert(ctx, f.db, f.database, tenant, p, p.String()); err != nil {
			t.Fatal(err)
		}
		draft := createAuthoringDraft(t, authoring, tenant, p)
		bindings = append(bindings, binding{p, draft.ID})
	}
	for start := 0; start < 5000; start += 100 {
		marks := make([]string, 100)
		args := make([]any, 0, 1100)
		for i := range marks {
			n := start + i
			object := fmt.Sprintf("OBJECT-%06d", n)
			if n%10 == 0 {
				object = "HOT"
			}
			marks[i] = "(?,?,?,?,?,?,?, ?,?,?,?)"
			bound := bindings[n%len(bindings)]
			args = append(args, mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), mysqlstore.UUID(bound.parent), mysqlstore.UUID(uuid.MustParse(bound.iteration)), mysqlstore.Text(object), mysqlstore.Text("wallets"), mysqlstore.JSON(`{}`), mysqlstore.Text("review"), 45, true, mysqlstore.DateTime{Time: at.Add(time.Duration(n) * time.Microsecond)})
		}
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,request_body,outcome,score,triggered,created_at) VALUES "+strings.Join(marks, ","), args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.ExecContext(ctx, "ANALYZE TABLE "+schema+".decision_engine_decisions"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		for _, test := range []struct {
			name, key string
			filter    ports.DecisionListFilter
			cursor    bool
		}{
			{"tenant first page", "de_decision_history", ports.DecisionListFilter{}, false},
			{"scenario first page", "de_decision_scenario", ports.DecisionListFilter{ScenarioID: parent.String()}, false},
			{"exact object", "de_decision_object", ports.DecisionListFilter{ObjectType: "wallets", ObjectID: "OBJECT-004999"}, false},
			{"hot object", "de_decision_object", ports.DecisionListFilter{ObjectType: "wallets", ObjectID: "HOT"}, false},
			{"tenant cursor", "de_decision_history", ports.DecisionListFilter{}, true},
		} {
			where, args, err := r.Decisions.filter(tenant.String(), test.filter)
			if err != nil {
				return err
			}
			if test.cursor {
				where += " AND (d.created_at<? OR (d.created_at=? AND d.id<?))"
				cut := mysqlstore.DateTime{Time: at.Add(2500 * time.Microsecond)}
				args = append(args, cut, cut, mysqlstore.UUID(uuid.Nil))
			}
			query := r.Decisions.selectSQL(false) + where + " ORDER BY d.created_at DESC,d.id DESC LIMIT 26"
			var plan string
			if err := r.Decisions.tx.QueryRowContext(ctx, "EXPLAIN ANALYZE "+query, args...).Scan(&plan); err != nil {
				return err
			}
			t.Logf("%s: %s", test.name, plan)
			if !strings.Contains(plan, test.key) || strings.Contains(strings.ToLower(plan), "table scan") {
				t.Fatalf("%s did not select expected bounded index: %s", test.name, plan)
			}
		}
		// Contains search is intentionally not called indexed. Capture the actual
		// residual-filter plan to keep that operational limitation visible.
		where, args, err := r.Decisions.filter(tenant.String(), ports.DecisionListFilter{Search: "OBJECT"})
		if err != nil {
			return err
		}
		var plan string
		if err := r.Decisions.tx.QueryRowContext(ctx, "EXPLAIN ANALYZE "+r.Decisions.selectSQL(false)+where+" ORDER BY d.created_at DESC,d.id DESC LIMIT 26", args...).Scan(&plan); err != nil {
			return err
		}
		t.Logf("contains search (residual filter, no text index guarantee): %s", plan)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
