package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type decisionTransactionFixture func(context.Context, func(ports.DecisionMutationStore) error) error

func (f decisionTransactionFixture) RunDecisionMutation(ctx context.Context, fn func(ports.DecisionMutationStore) error) error {
	return f(ctx, fn)
}

func TestDecisionUsesConfiguredNarrowTransactionsWithoutFallback(t *testing.T) {
	failed := errors.New("target commit outcome requires reconciliation")
	s := newFailureTestDecisionService(txManagerStub{store: decisionMutationStoreStub{}}, nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), nilScreeningConfigRepository(nil))
	if _, err := s.WithDecisionTransactions(nil); err == nil {
		t.Fatal("missing manager accepted")
	}
	called := 0
	target, err := s.WithDecisionTransactions(decisionTransactionFixture(func(context.Context, func(ports.DecisionMutationStore) error) error { called++; return failed }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", failureTestEvaluationRequest()); !errors.Is(err, failed) || called != 1 {
		t.Fatal("target failure hidden or source fallback used", called, err)
	}
	if s.decisionTransactions != nil {
		t.Fatal("configuration mutated source service")
	}
}
