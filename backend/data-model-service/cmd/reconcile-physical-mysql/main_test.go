package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRecoveryCommandValidation(t *testing.T) {
	base := []string{"-address", "mysql:3306", "-database", "fixture", "-user", "recovery", "-tenant", "00000000-0000-0000-0000-000000000001", "-state", "prepared"}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-state", "published"}, "pending intent state"},
		{[]string{"-tenant", "00000000-0000-0000-0000-000000000000"}, "nonzero tenant"},
		{[]string{"-after", "not-a-uuid"}, "cursor UUID"},
		{[]string{"-limit", "1001"}, "bounded page"},
		{[]string{"-operation-timeout", "0s"}, "positive deadlines"},
		{[]string{"-timeout", "1s"}, "operation deadline"},
		{[]string{"-address", "invalid"}, "host:port"},
		{[]string{"-database", "unsafe;sql"}, "identifier"},
		{[]string{"unexpected"}, "explicit account"},
	} {
		var out bytes.Buffer
		err := run(context.Background(), append(append([]string{}, base...), tc.args...), &out)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
	}
	t.Setenv("DATA_MODEL_MYSQL_RECOVERY_PASSWORD", "fixture-secret")
	t.Setenv("DATA_MODEL_REQUIRE_MYSQL_TEST", "")
	err := run(context.Background(), append(base, "-isolated-fixture"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "plaintext allowed only") {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-help"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}
