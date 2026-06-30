package ast_eval

import "testing"

func TestCompareValuesHandlesNilOperands(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		op    string
		left  any
		right any
		want  bool
	}{
		{name: "eq both nil", op: "eq", left: nil, right: nil, want: true},
		{name: "eq left nil", op: "eq", left: nil, right: 10, want: false},
		{name: "neq left nil", op: "neq", left: nil, right: 10, want: true},
		{name: "gt left nil", op: "gt", left: nil, right: 10, want: false},
		{name: "gte right nil", op: "gte", left: 10, right: nil, want: false},
		{name: "lt right nil", op: "lt", left: 10, right: nil, want: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := compareValues(tc.op, tc.left, tc.right)
			if err != nil {
				t.Fatalf("compareValues(%q, %v, %v) error = %v", tc.op, tc.left, tc.right, err)
			}
			if got != tc.want {
				t.Fatalf("compareValues(%q, %v, %v) = %v, want %v", tc.op, tc.left, tc.right, got, tc.want)
			}
		})
	}
}
