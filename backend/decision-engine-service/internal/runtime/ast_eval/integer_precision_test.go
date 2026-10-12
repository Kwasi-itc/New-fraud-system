package ast_eval

import (
	"encoding/json"
	"testing"
)

func TestPreparedIntegerComparisonPreservesPrecision(t *testing.T) {
	for _, tc := range []struct {
		op          string
		left, right any
		want        bool
	}{
		{"eq", int64(9007199254740993), int64(9007199254740992), false},
		{"gt", int64(9007199254740993), int64(9007199254740992), true},
		{"eq", int64(9007199254740993), float64(9007199254740992), false},
		{"lt", float64(9007199254740992), int64(9007199254740993), true},
		{"eq", json.Number("9223372036854775807"), int64(9223372036854775807), true},
	} {
		got, err := compareValues(tc.op, tc.left, tc.right)
		if err != nil || got != tc.want {
			t.Fatalf("%s %v %v: %v %v", tc.op, tc.left, tc.right, got, err)
		}
	}
}
