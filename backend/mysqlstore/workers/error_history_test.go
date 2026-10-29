package workers

import (
	"errors"
	"testing"
)

func TestErrorHistoryCannotBeInventedFromClaims(t *testing.T) {
	zero, one, two, negative := int64(0), int64(1), int64(2), int64(-1)
	for _, test := range []struct {
		attempt int
		count   *int64
		record  bool
		valid   bool
	}{{0, &zero, false, true}, {1, &zero, true, true}, {3, &one, true, true}, {1, &one, false, true}, {0, nil, false, false}, {3, nil, false, false}, {0, &zero, true, false}, {1, &one, true, false}, {1, &two, false, false}, {1, &negative, false, false}, {-1, &zero, false, false}} {
		err := ValidateErrorHistory(test.attempt, test.count, test.record)
		if (err == nil) != test.valid || (!test.valid && !errors.Is(err, ErrErrorHistory)) {
			t.Fatalf("attempt=%d recording=%t: %v", test.attempt, test.record, err)
		}
	}
}
