package mysqlstore

import (
	"math"
	"testing"
)

func TestConnectionBudget(t *testing.T) {
	got, err := ValidateConnectionBudget(100, 10, []PoolBudget{{"decision_api", 2, 24}, {"ingestion_api", 2, 12}, {"workers", 3, 4}})
	if err != nil || got.ConfiguredMaximum != 84 || got.Unallocated != 6 {
		t.Fatalf("budget: %+v %v", got, err)
	}
	for _, test := range []struct {
		limit, reserved int64
		pools           []PoolBudget
	}{
		{0, 0, []PoolBudget{{"api", 1, 1}}}, {10, -1, []PoolBudget{{"api", 1, 1}}}, {10, 10, []PoolBudget{{"api", 1, 1}}}, {10, 1, nil},
		{10, 1, []PoolBudget{{"api", 1, 0}}}, {10, 1, []PoolBudget{{"api", 0, 1}}}, {10, 1, []PoolBudget{{"api", 1, -1}}},
		{10, 1, []PoolBudget{{"api", 1, 5}, {"api", 1, 1}}}, {10, 1, []PoolBudget{{"password@host", 1, 1}}},
		{10, 1, []PoolBudget{{"api", 2, 5}}}, {math.MaxInt64, 0, []PoolBudget{{"api", math.MaxInt64, math.MaxInt64}}},
	} {
		if _, err := ValidateConnectionBudget(test.limit, test.reserved, test.pools); err == nil {
			t.Fatalf("invalid budget accepted: %+v", test)
		}
	}
}
