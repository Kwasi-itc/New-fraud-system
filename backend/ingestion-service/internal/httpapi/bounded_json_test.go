package httpapi

import (
	"strings"
	"testing"
)

func TestBoundedJSONBeforeHandlerTraversal(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"value":"escaped \\\" [ { ] }"}`, true},
		{`{"value":1} {"ignored":2}`, false},
		{`{"value":1} trailing`, false},
		{`{"value":`, false},
		{strings.Repeat("[", 64) + "0" + strings.Repeat("]", 64), true},
		{strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65), false},
	} {
		if got := validBoundedJSON([]byte(tc.body), 64); got != tc.valid {
			t.Fatalf("valid=%v want=%v body=%q", got, tc.valid, tc.body)
		}
	}
}
