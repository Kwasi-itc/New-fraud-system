package postgres

import (
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestBuildDecisionFilterWhereClauseUsesExactMatchForObjectFilters(t *testing.T) {
	t.Parallel()

	joinSQL, whereSQL, args := buildDecisionFilterWhereClause(ports.DecisionListFilter{
		ObjectType: "transactions",
		ObjectID:   "txn_123",
	}, 2)

	if joinSQL != "" {
		t.Fatalf("joinSQL = %q, want empty", joinSQL)
	}
	if !strings.Contains(whereSQL, "d.object_type = $2") {
		t.Fatalf("whereSQL = %q, want exact object_type predicate", whereSQL)
	}
	if !strings.Contains(whereSQL, "d.object_id = $3") {
		t.Fatalf("whereSQL = %q, want exact object_id predicate", whereSQL)
	}
	if strings.Contains(strings.ToLower(whereSQL), "ilike") {
		t.Fatalf("whereSQL = %q, want no ilike predicates for object filters", whereSQL)
	}
	if len(args) != 2 || args[0] != "transactions" || args[1] != "txn_123" {
		t.Fatalf("args = %#v, want exact object filter args", args)
	}
}

func TestBuildDecisionFilterWhereClauseSupportsDecisionIDAndObjectIDPrefix(t *testing.T) {
	t.Parallel()

	joinSQL, whereSQL, args := buildDecisionFilterWhereClause(ports.DecisionListFilter{
		DecisionID:     "11111111-1111-1111-1111-111111111111",
		ObjectIDPrefix: "txn_",
	}, 2)

	if joinSQL != "" {
		t.Fatalf("joinSQL = %q, want empty", joinSQL)
	}
	if !strings.Contains(whereSQL, "d.id = $2::uuid") {
		t.Fatalf("whereSQL = %q, want exact decision id predicate", whereSQL)
	}
	if !strings.Contains(whereSQL, "d.object_id like $3") {
		t.Fatalf("whereSQL = %q, want object_id prefix predicate", whereSQL)
	}
	if len(args) != 2 || args[0] != "11111111-1111-1111-1111-111111111111" || args[1] != "txn_%" {
		t.Fatalf("args = %#v, want decision id and prefix args", args)
	}
}

func TestBuildDecisionFilterWhereClauseKeepsScenarioJoinConditionalForSearch(t *testing.T) {
	t.Parallel()

	joinSQL, whereSQL, args := buildDecisionFilterWhereClause(ports.DecisionListFilter{
		Search: "merchant-1",
	}, 2)

	if !strings.Contains(joinSQL, "left join core.scenarios") {
		t.Fatalf("joinSQL = %q, want conditional scenario join", joinSQL)
	}
	if !strings.Contains(whereSQL, "coalesce(s.name, '') ilike $2") {
		t.Fatalf("whereSQL = %q, want scenario-name search predicate", whereSQL)
	}
	if len(args) != 1 || args[0] != "%merchant-1%" {
		t.Fatalf("args = %#v, want fuzzy search arg", args)
	}
}
