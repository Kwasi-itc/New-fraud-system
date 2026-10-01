package casepkg

import (
	"testing"
	"time"
)

func TestSLAUTCDateBoundaries(t *testing.T) {
	one := 1
	for _, test := range []struct{ created, want string }{
		{"2024-02-28T23:30:00Z", "2024-02-29T23:30:00Z"},
		{"2026-12-31T23:30:00-05:00", "2027-01-02T04:30:00Z"},
		{"2026-03-29T00:30:00+01:00", "2026-03-29T23:30:00Z"},
		{"2026-10-25T00:30:00+02:00", "2026-10-25T22:30:00Z"},
	} {
		created, err := time.Parse(time.RFC3339, test.created)
		if err != nil {
			t.Fatal(err)
		}
		due := SLADueAt(created, &one)
		if due == nil || due.Format(time.RFC3339) != test.want || due.Location() != time.UTC {
			t.Fatal(test, due)
		}
	}
	if SLADueAt(time.Now(), nil) != nil {
		t.Fatal("disabled SLA has deadline")
	}
}
