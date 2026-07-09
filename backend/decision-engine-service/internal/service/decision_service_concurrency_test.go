package service

import "testing"

func TestResolveScenarioEvaluationConcurrency(t *testing.T) {
	t.Parallel()

	if got := resolveScenarioEvaluationConcurrency(0, 3); got < 1 || got > 3 {
		t.Fatalf("resolveScenarioEvaluationConcurrency(default, 3) = %d, want within [1,3]", got)
	}
	if got := resolveScenarioEvaluationConcurrency(10, 3); got != 3 {
		t.Fatalf("resolveScenarioEvaluationConcurrency(10, 3) = %d, want 3", got)
	}
	if got := resolveScenarioEvaluationConcurrency(2, 3); got != 2 {
		t.Fatalf("resolveScenarioEvaluationConcurrency(2, 3) = %d, want 2", got)
	}
}
