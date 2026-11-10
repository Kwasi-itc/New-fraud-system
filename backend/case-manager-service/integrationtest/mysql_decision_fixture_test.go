package integrationtest

import (
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

// Seed only the private owner fixture, never the shared development database.
func seedCaseDecision(t *testing.T, f *caseFixture) (uuid.UUID, uuid.UUID) {
	t.Helper()
	if !strings.HasPrefix(f.database, "case_fixture_") {
		t.Fatal("private case fixture required")
	}
	scenario, iteration, decision := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	for _, v := range []struct {
		q string
		a []any
	}{
		{"INSERT INTO `" + f.database + "`.decision_engine_scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES(?,?,?,'transactions',?,?)", []any{id(scenario), id(f.tenant), "High amount transfers " + scenario.String(), now, now}},
		{"INSERT INTO `" + f.database + "`.decision_engine_scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES(?,?,?,1,'draft',?)", []any{id(iteration), id(scenario), id(f.tenant), now}},
		{"INSERT INTO `" + f.database + "`.decision_engine_decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,score,triggered,created_at) VALUES(?,?,?,?,'synthetic-transfer-001','transactions','review',85,TRUE,?)", []any{id(decision), id(f.tenant), id(scenario), id(iteration), now}},
	} {
		_, e := f.db.ExecContext(f.ctx, v.q, v.a...)
		must(t, e)
	}
	return decision, scenario
}
