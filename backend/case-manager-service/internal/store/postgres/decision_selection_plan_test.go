package postgres_test

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

type selectionTrace struct {
	sql     string
	args    []any
	queries int
}

func (s *selectionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "WITH selected AS") {
		s.sql = data.SQL
		s.args = data.Args
		s.queries++
	}
	return ctx
}
func (*selectionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestDecisionSelectionsBoundedQueryPlan(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, _, c := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `WITH added AS (
	 INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at)
	 SELECT gen_random_uuid(),d.tenant_id,d.scenario_id,d.scenario_iteration_id,'load-'||n,'customer','review',now()
	 FROM core.decisions d CROSS JOIN generate_series(1,50000) n WHERE d.id=$1 RETURNING id,tenant_id)
	 INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,created_at)
	 SELECT gen_random_uuid(),tenant_id,$2,id,now() FROM added`, decision, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE core.decisions; ANALYZE case_manager.case_decisions; ANALYZE case_manager.cases`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `SELECT decision_id FROM case_manager.case_decisions WHERE tenant_id=$1 LIMIT 100`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}
	trace := &selectionTrace{}
	cfg := db.Config()
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := service.NewCaseService(ids{}, clock{}, store.NewRepositories(pool), store.NewUnitOfWork(pool))
	for _, n := range []int{1, 25, 100} {
		trace.queries = 0
		got, err := s.DecisionSelections(userContext(tenant, "analyst-1"), tenant, selected[:n])
		if err != nil || len(got) != n || trace.queries != 1 {
			t.Fatalf("batch %d: %d queries, %d rows, %v", n, trace.queries, len(got), err)
		}
	}
	planRows, err := db.Query(ctx, "EXPLAIN (ANALYZE,BUFFERS) "+trace.sql, trace.args...)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(planRows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, "Index") || strings.Contains(plan, "Seq Scan on decisions") || strings.Contains(plan, "Seq Scan on case_decisions") {
		t.Fatalf("unexpected owner/link scan:\n%s", plan)
	}
	t.Log(plan)
}
