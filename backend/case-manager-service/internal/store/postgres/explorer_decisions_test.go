package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestAssociatedDecisionsVisibilityPaginationAndAttachment(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var schema string
	if err := db.QueryRow(ctx, `SELECT schema_name FROM core.tenants WHERE id=$1`, tenant).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	table := uuid.New()
	exec(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'customer',now(),now())`, table, tenant)
	exec("CREATE SCHEMA " + pgx.Identifier{schema}.Sanitize())
	exec("CREATE TABLE " + pgx.Identifier{schema, "customer"}.Sanitize() + " (object_id text PRIMARY KEY)")
	exec("INSERT INTO " + pgx.Identifier{schema, "customer"}.Sanitize() + " VALUES ('customer-1')")
	root := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: root}, nil); err != nil {
		t.Fatal(err)
	}
	privateInbox, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Private inbox"})
	if err != nil {
		t.Fatal(err)
	}
	privateCase, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: privateInbox.ID, Name: "Confidential investigation"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	visibleCase, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Visible investigation"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hidden := map[uuid.UUID]bool{}
	targets := []uuid.UUID{}
	for n := 0; n < 70; n++ {
		id := uuid.New()
		seconds := n + 1
		if n >= 35 {
			seconds = -seconds
		}
		exec(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,score,created_at) SELECT $2,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,'review',42,now()+($3 * interval '1 second') FROM core.decisions WHERE id=$1`, root, id, seconds)
		if n < 35 {
			hidden[id] = true
			exec(`INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,created_at) SELECT gen_random_uuid(),tenant_id,$2,id,scenario_id,object_type,object_id,now() FROM core.decisions WHERE id=$1`, id, privateCase.ID)
		} else {
			targets = append(targets, id)
		}
	}
	if _, err = s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: visibleCase.ID, DecisionID: targets[0]}, nil); err != nil {
		t.Fatal(err)
	}
	in := casepkg.ExplorationRequest{DecisionID: root}
	seen := map[uuid.UUID]bool{}
	foundCase := false
	for {
		out, e := s.Explore(ctx, tenant, c.ID, in)
		if e != nil {
			t.Fatal(e)
		}
		if len(out.Decisions) > 25 {
			t.Fatal("unbounded page")
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), privateCase.ID.String()) || strings.Contains(string(raw), privateCase.Name) {
			t.Fatal("private case leaked")
		}
		for _, d := range out.Decisions {
			if seen[d.ID] || hidden[d.ID] {
				t.Fatal("duplicate or inaccessible decision")
			}
			seen[d.ID] = true
			if d.ID == targets[0] {
				foundCase = d.Case != nil && d.Case.ID == visibleCase.ID && !d.CanAttach
			}
			if d.ID == targets[1] && !d.CanAttach {
				t.Fatal("unlinked decision not attachable")
			}
		}
		if out.NextDecision == "" {
			break
		}
		in.DecisionAfter = out.NextDecision
	}
	if len(seen) != 36 || !foundCase {
		t.Fatal("visibility/pagination", len(seen), foundCase)
	}
	in.DecisionAfter = ""
	adminPage, err := s.Explore(adminContext(tenant), tenant, c.ID, in)
	if err != nil || len(adminPage.Decisions) != 25 || adminPage.Decisions[0].Case == nil || adminPage.Decisions[0].Case.ID != privateCase.ID {
		t.Fatal("administrator visibility", err)
	}
	// Attach concurrent retries once, with the same existing audit/lifecycle rules.
	input := casepkg.ExplorationAttach{ExplorationRequest: in, TargetDecisionID: targets[1]}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.AttachExploredDecision(ctx, tenant, c.ID, input); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent replay", e)
		}
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_decisions WHERE tenant_id=$1 AND decision_id=$2`, tenant, targets[1]).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate membership", err, count)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE tenant_id=$1 AND case_id=$2 AND event_type='decision_added' AND resource_type='decision' AND resource_id=$3`, tenant, c.ID, targets[1].String()).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate event", err, count)
	}
	for _, target := range []uuid.UUID{targets[0], func() uuid.UUID {
		for id := range hidden {
			return id
		}
		return uuid.Nil
	}()} {
		input.TargetDecisionID = target
		if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, input); !errors.Is(err, casepkg.ErrConflict) {
			t.Fatal("implicit case transfer", err)
		}
	}
	unrelated := seedDecision(t, db, tenant)
	exec(`UPDATE core.decisions SET object_id='other-object' WHERE id=$1`, unrelated)
	input.TargetDecisionID = unrelated
	if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, input); !errors.Is(err, casepkg.ErrNotFound) {
		t.Fatal("unrelated decision attached", err)
	}
	otherTenant := uuid.New()
	seedTenant(t, db, otherTenant)
	input.TargetDecisionID = seedDecision(t, db, otherTenant)
	if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, input); err == nil {
		t.Fatal("foreign decision attached")
	}
	input.TargetDecisionID = targets[2]
	if _, err = s.AttachExploredDecision(userContext(tenant, "outsider"), tenant, c.ID, input); err == nil {
		t.Fatal("unauthorized attachment")
	}
	if _, err = s.AttachExploredDecision(serviceContext(tenant), tenant, c.ID, input); err == nil {
		t.Fatal("machine attachment")
	}
	failInserts(t, db, "case_events")
	if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, input); err == nil {
		t.Fatal("audit failure committed")
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_decisions WHERE decision_id=$1`, targets[2]).Scan(&count); err != nil || count != 0 {
		t.Fatal("attachment escaped rollback", err)
	}
	exec(`DROP TRIGGER fail_test_write ON case_manager.case_events`)
	exec(`UPDATE case_manager.cases SET status='closed',outcome='false_positive' WHERE id=$1`, c.ID)
	if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, input); err == nil {
		t.Fatal("closed case attachment")
	}
	input.TargetDecisionID = targets[1]
	if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, input); err != nil {
		t.Fatal("authorized replay after closure", err)
	}
	in.DecisionAfter = "invalid"
	if _, err = s.Explore(ctx, tenant, c.ID, in); err == nil {
		t.Fatal("invalid cursor")
	}
}

func TestAssociatedDecisionIndexedLookupPlan(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	root := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: root}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) SELECT gen_random_uuid(),d.tenant_id,d.scenario_id,d.scenario_iteration_id,'object-'||(n%1000),d.object_type,'review',now()-(n * interval '1 second') FROM core.decisions d CROSS JOIN generate_series(1,50000)n WHERE d.id=$1`, root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE core.decisions`); err != nil {
		t.Fatal(err)
	}
	var plan json.RawMessage
	// Same owner index and keyset order used by discovery; membership visibility
	// correctness is separately exercised above with more than a page of hidden rows.
	if err := db.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id FROM core.decisions WHERE tenant_id=$1 AND object_type='customer' AND object_id='object-42' ORDER BY created_at DESC,id DESC LIMIT 26`, tenant).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), "decisions_tenant_object_lookup_created_idx") {
		t.Fatal("missing object lookup index", string(plan))
	}
	t.Log(fmt.Sprintf("50,000-decision lookup: %s", plan))
}
