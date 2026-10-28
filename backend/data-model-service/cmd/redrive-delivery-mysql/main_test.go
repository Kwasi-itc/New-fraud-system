package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRedriveCommandValidation(t *testing.T) {
	base := []string{"-apply", "-address", "localhost:3306", "-database", "shared_app", "-user", "operator", "-tenant", "11111111-1111-1111-1111-111111111111", "-job", "22222222-2222-2222-2222-222222222222", "-source", "33333333-3333-3333-3333-333333333333", "-request", "44444444-4444-4444-4444-444444444444", "-actor", "operator", "-reason", "repair", "-scheduled-at", "2026-10-27T00:00:00Z", "-queue", "index_jobs", "-max-attempts", "5"}
	t.Setenv("DATA_MODEL_MYSQL_REDRIVE_PASSWORD", "fixture-only")
	for _, tail := range [][]string{{"-apply=false"}, {"-tenant", ""}, {"-job", ""}, {"-request", ""}, {"-source", ""}, {"-actor", ""}, {"-reason", ""}, {"-scheduled-at", "2026-10-27T00:00:00.0000001Z"}, {"-timeout", "0s"}, {"-max-attempts", "0"}, {"-database", "invalid-name"}, {"-password-env", "DATA_MODEL_MISSING_REDRIVE_PASSWORD"}, {"-ca", "missing-ca.pem"}, {"-isolated-fixture"}} {
		var out bytes.Buffer
		args := append(append([]string{}, base...), tail...)
		if err := run(context.Background(), args, &out); err == nil {
			t.Fatalf("accepted %v", tail)
		}
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-help"}, &out); err != nil || out.Len() == 0 {
		t.Fatalf("help %s %v", out.String(), err)
	}
}
