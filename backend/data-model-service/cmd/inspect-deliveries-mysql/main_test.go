package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestInspectionCommandValidation(t *testing.T) {
	base := []string{"-address", "mysql:3306", "-database", "fixture", "-user", "inspector", "-tenant", "00000000-0000-0000-0000-000000000001"}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-limit", "1001"}, "bounded page"},
		{[]string{"-timeout", "0s"}, "positive timeout"},
		{[]string{"-tenant", "00000000-0000-0000-0000-000000000000"}, "tenant UUID"},
		{[]string{"-delivery", "invalid"}, "delivery UUID"},
		{[]string{"-before-id", "00000000-0000-0000-0000-000000000001"}, "cursor timestamp"},
		{[]string{"-before-created-at", "2026-10-27T00:00:00Z"}, "cursor UUID"},
		{[]string{"-before-id", "00000000-0000-0000-0000-000000000001", "-before-created-at", "2026-10-27T00:00:00.000000001Z"}, "microseconds"},
		{[]string{"-delivery", "00000000-0000-0000-0000-000000000001", "-before-id", "00000000-0000-0000-0000-000000000001"}, "cannot use a page cursor"},
		{[]string{"-address", "invalid"}, "host:port"},
	} {
		err := run(context.Background(), append(append([]string{}, base...), tc.args...), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
	}
	t.Setenv("DATA_MODEL_MYSQL_INSPECTION_PASSWORD", "fixture-only")
	t.Setenv("DATA_MODEL_REQUIRE_MYSQL_TEST", "")
	if err := run(context.Background(), append(base, "-isolated-fixture"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "plaintext allowed only") {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-help"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}
