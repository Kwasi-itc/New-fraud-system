package ast_eval

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type spyAggregateTenantDataReader struct {
	calls atomic.Int32
	val   any
	err   error
}

func (s *spyAggregateTenantDataReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}
func (s *spyAggregateTenantDataReader) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (s *spyAggregateTenantDataReader) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (s *spyAggregateTenantDataReader) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.val, nil
}

type spyCountDecisionRepo struct {
	listCalled   atomic.Int32
	countCalled  atomic.Int32
	countOutcome atomic.Int32
}

func (s *spyCountDecisionRepo) Create(context.Context, decision.Decision) (decision.Decision, error) {
	return decision.Decision{}, nil
}
func (s *spyCountDecisionRepo) GetByID(context.Context, string, string) (decision.Decision, error) {
	return decision.Decision{}, nil
}
func (s *spyCountDecisionRepo) ListByTenant(context.Context, string) ([]decision.Decision, error) {
	return nil, nil
}
func (s *spyCountDecisionRepo) ListByTenantPage(context.Context, string, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (s *spyCountDecisionRepo) CountByTenant(context.Context, string) (int, error) { return 0, nil }
func (s *spyCountDecisionRepo) ListByScenario(context.Context, string, string) ([]decision.Decision, error) {
	return nil, nil
}
func (s *spyCountDecisionRepo) ListByScenarioPage(context.Context, string, string, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (s *spyCountDecisionRepo) CountByScenario(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *spyCountDecisionRepo) ListByObject(context.Context, string, string, string) ([]decision.Decision, error) {
	s.listCalled.Add(1)
	return nil, errors.New("ListByObject should not be called by past_decision_count")
}
func (s *spyCountDecisionRepo) ListByObjectPage(context.Context, string, string, string, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (s *spyCountDecisionRepo) CountByObject(context.Context, string, string, string) (int, error) {
	s.countCalled.Add(1)
	return 7, nil
}
func (s *spyCountDecisionRepo) CountByObjectOutcome(context.Context, string, string, string, string) (int, error) {
	s.countOutcome.Add(1)
	return 3, nil
}
func (s *spyCountDecisionRepo) ListFiltered(context.Context, string, ports.DecisionListFilter) ([]decision.Decision, error) {
	return nil, nil
}
func (s *spyCountDecisionRepo) ListFilteredPage(context.Context, string, ports.DecisionListFilter, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (s *spyCountDecisionRepo) ListFilteredCursor(context.Context, string, ports.DecisionListFilter, int, *ports.DecisionListCursor) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (s *spyCountDecisionRepo) CountFiltered(context.Context, string, ports.DecisionListFilter) (int, error) {
	return 0, nil
}

func makeSumAggregatorNode(table, field string) domainast.Node {
	return domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: table},
					"fieldName":  {Constant: field},
					"aggregator": {Constant: "SUM"},
				},
			},
			{Constant: 100000.0},
		},
	}
}

func TestV2KleeneShortCircuitsAggregatorAndPreservesAuthoredOrder(t *testing.T) {
	t.Parallel()

	formulaNode := domainast.Node{
		Function: "and",
		Children: []domainast.Node{
			makeSumAggregatorNode("transactions", "amount"),
			{
				Function: "gt",
				Children: []domainast.Node{
					{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "amount"}}},
					{Constant: 1000.0},
				},
			},
		},
	}
	raw, _ := json.Marshal(formulaNode)
	reader := &spyAggregateTenantDataReader{val: 500000.0}
	rt := Runtime{
		TenantID:            "t1",
		ObjectID:            "txn-1",
		ObjectType:          "transactions",
		Fields:              map[string]any{"amount": 50.0},
		Now:                 time.Now().UTC(),
		TenantDataReader:    reader,
		EvaluationSemantics: "v2",
		AggregatePushdownMode: AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"sum"},
	}

	beforeSkips := AggregatePushdownMetricsSnapshot().ShortCircuitSkipsTotal
	matched, err := EvaluateFormula(context.Background(), raw, rt)
	if err != nil {
		t.Fatalf("EvaluateFormula() error = %v", err)
	}
	if matched {
		t.Fatalf("EvaluateFormula() = true, want false")
	}
	if reader.calls.Load() != 0 {
		t.Fatalf("AggregateRecords called %d times, want 0", reader.calls.Load())
	}

	matchedEv, evalNode, err := EvaluateFormulaWithEvidence(context.Background(), raw, rt)
	if err != nil {
		t.Fatalf("EvaluateFormulaWithEvidence() error = %v", err)
	}
	if matchedEv {
		t.Fatalf("EvaluateFormulaWithEvidence() = true, want false")
	}
	if reader.calls.Load() != 0 {
		t.Fatalf("AggregateRecords called %d times during evidence eval, want 0", reader.calls.Load())
	}
	if len(evalNode.Children) != 2 {
		t.Fatalf("len(evalNode.Children) = %d, want 2", len(evalNode.Children))
	}
	if !evalNode.Children[0].Skipped || evalNode.Children[0].Function != "gt" {
		t.Fatalf("expected child 0 (Aggregator > 100k) to stay at index 0 and be Skipped=true, got %+v", evalNode.Children[0])
	}
	if evalNode.Children[1].Skipped || evalNode.Children[1].ReturnValue != false {
		t.Fatalf("expected child 1 (amount > 1000) to stay at index 1 with ReturnValue=false, got %+v", evalNode.Children[1])
	}
	if AggregatePushdownMetricsSnapshot().ShortCircuitSkipsTotal < beforeSkips+2 {
		t.Fatalf("expected ShortCircuitSkipsTotal to increment")
	}
}

func TestV2KleeneDefersNullAcrossTier0AndStillShortCircuitsFalse(t *testing.T) {
	t.Parallel()

	formulaNode := domainast.Node{
		Function: "and",
		Children: []domainast.Node{
			{
				Function: "contains",
				Children: []domainast.Node{
					{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "optional_field"}}},
					{Constant: "vip"},
				},
			},
			{
				Function: "gt",
				Children: []domainast.Node{
					{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "amount"}}},
					{Constant: 1000.0},
				},
			},
			makeSumAggregatorNode("transactions", "amount"),
		},
	}
	raw, _ := json.Marshal(formulaNode)
	reader := &spyAggregateTenantDataReader{val: 500000.0}
	rt := Runtime{
		TenantID:                    "t1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{"optional_field": nil, "amount": 50.0},
		Now:                         time.Now().UTC(),
		TenantDataReader:            reader,
		EvaluationSemantics:         "v2",
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"sum"},
	}

	matched, err := EvaluateFormula(context.Background(), raw, rt)
	if err != nil {
		t.Fatalf("EvaluateFormula() error = %v, want nil (Kleene false)", err)
	}
	if matched {
		t.Fatalf("EvaluateFormula() = true, want false")
	}
	if reader.calls.Load() != 0 {
		t.Fatalf("AggregateRecords called %d times, want 0", reader.calls.Load())
	}

	matchedEv, evalNode, err := EvaluateFormulaWithEvidence(context.Background(), raw, rt)
	if err != nil || matchedEv {
		t.Fatalf("EvaluateFormulaWithEvidence() = (%v, %v), want (false, nil)", matchedEv, err)
	}
	if !evalNode.Children[2].Skipped {
		t.Fatalf("expected child 2 (Aggregator) to be marked Skipped=true")
	}
}

func TestV2KleeneDoesNotDispatchAggregatorAfterNullOperandError(t *testing.T) {
	t.Parallel()

	formulaNode := domainast.Node{
		Function: "and",
		Children: []domainast.Node{
			{
				Function: "contains",
				Children: []domainast.Node{
					{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "optional_field"}}},
					{Constant: "vip"},
				},
			},
			makeSumAggregatorNode("transactions", "amount"),
		},
	}
	raw, _ := json.Marshal(formulaNode)
	reader := &spyAggregateTenantDataReader{val: 500000.0}
	rt := Runtime{
		TenantID:                    "t1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{"optional_field": nil},
		Now:                         time.Now().UTC(),
		TenantDataReader:            reader,
		EvaluationSemantics:         "v2",
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"sum"},
	}

	_, err := EvaluateFormula(context.Background(), raw, rt)
	var nullErr *NullOperandError
	if !errors.As(err, &nullErr) {
		t.Fatalf("EvaluateFormula() error = %v, want *NullOperandError", err)
	}
	if reader.calls.Load() != 0 {
		t.Fatalf("AggregateRecords called %d times after NullOperandError, want 0", reader.calls.Load())
	}

	_, _, err = EvaluateFormulaWithEvidence(context.Background(), raw, rt)
	if !errors.As(err, &nullErr) {
		t.Fatalf("EvaluateFormulaWithEvidence() error = %v, want *NullOperandError", err)
	}
	if reader.calls.Load() != 0 {
		t.Fatalf("AggregateRecords called %d times in evidence eval after NullOperandError, want 0", reader.calls.Load())
	}
}

func TestPastDecisionCountUsesCountByObjectWithoutListByObject(t *testing.T) {
	t.Parallel()

	repo := &spyCountDecisionRepo{}
	rt := Runtime{
		TenantID:     "t1",
		ObjectID:     "obj-1",
		ObjectType:   "transactions",
		DecisionRepo: repo,
	}

	valAll, err := EvaluateNode(context.Background(), domainast.Node{Function: "past_decision_count"}, rt)
	if err != nil || valAll != float64(7) {
		t.Fatalf("past_decision_count (all) = (%v, %v), want (7, nil)", valAll, err)
	}
	valReview, err := EvaluateNode(context.Background(), domainast.Node{
		Function:      "past_decision_count",
		NamedChildren: map[string]domainast.Node{"outcome": {Constant: "review"}},
	}, rt)
	if err != nil || valReview != float64(3) {
		t.Fatalf("past_decision_count (review) = (%v, %v), want (3, nil)", valReview, err)
	}
	if repo.listCalled.Load() != 0 {
		t.Fatalf("ListByObject called %d times, want 0", repo.listCalled.Load())
	}
	if repo.countCalled.Load() != 1 || repo.countOutcome.Load() != 1 {
		t.Fatalf("unexpected counts: all=%d outcome=%d", repo.countCalled.Load(), repo.countOutcome.Load())
	}
}

func TestLintAggregatorWarnings(t *testing.T) {
	t.Parallel()

	unfilteredCountDistinct := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "merchant_id"},
			"aggregator": {Constant: "COUNT_DISTINCT"},
			"filters": {
				Function: "filter",
				NamedChildren: map[string]domainast.Node{
					"fieldName": {Constant: "created_at"},
					"operator":  {Constant: "gte"},
					"value": {
						Function: "time_add",
						NamedChildren: map[string]domainast.Node{
							"time":   {Function: "time_now"},
							"amount": {Constant: -48},
							"unit":   {Constant: "hours"},
						},
					},
				},
			},
		},
	}
	warnings := LintAggregatorWarnings(unfilteredCountDistinct)
	if len(warnings) != 2 {
		t.Fatalf("got %d warnings (%v), want 2 (missing eq + >24h COUNT_DISTINCT)", len(warnings), warnings)
	}

	cleanCountDistinct := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "merchant_id"},
			"aggregator": {Constant: "COUNT_DISTINCT"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{
						Function: "filter",
						NamedChildren: map[string]domainast.Node{
							"fieldName": {Constant: "account_id"},
							"operator":  {Constant: "eq"},
							"value":     {Constant: "acct-1"},
						},
					},
					{
						Function: "filter",
						NamedChildren: map[string]domainast.Node{
							"fieldName": {Constant: "created_at"},
							"operator":  {Constant: "gte"},
							"value": {
								Function: "time_add",
								NamedChildren: map[string]domainast.Node{
									"time":   {Function: "time_now"},
									"amount": {Constant: -24},
									"unit":   {Constant: "hours"},
								},
							},
						},
					},
				},
			},
		},
	}
	if cleanWarnings := LintAggregatorWarnings(cleanCountDistinct); len(cleanWarnings) != 0 {
		t.Fatalf("expected 0 warnings for clean 24h COUNT_DISTINCT with eq filter, got %v", cleanWarnings)
	}
}
