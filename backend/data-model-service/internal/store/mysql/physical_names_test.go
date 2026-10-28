package mysql

import "testing"

func TestPhysicalLabelSafety(t *testing.T) {
	for _, tc := range []struct {
		input          string
		limit          int
		fallback, want string
	}{
		{"ACME Payments", 10, "tenant", "acme_payme"},
		{" Customer Accounts ", 12, "table", "customer_acc"},
		{"email`; DROP TABLE users; --", 12, "field", "email_drop_t"},
		{"客户", 12, "table", "table"},
		{"____", 12, "field", "field"},
		{"abcdefghijk name", 12, "field", "abcdefghijk"},
	} {
		if got := physicalLabel(tc.input, tc.limit, tc.fallback); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.input, got, tc.want)
		}
	}
}
