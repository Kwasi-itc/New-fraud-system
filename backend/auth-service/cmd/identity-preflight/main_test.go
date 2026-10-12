package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestListSourcesDoesNotReadCredentials(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"-list-sources"}, func(string) string { t.Fatal("unexpected credential lookup"); return "" }, &out, &out)
	if err != nil || !strings.Contains(out.String(), "decision_review_actors") {
		t.Fatalf("list: %s %v", out.String(), err)
	}
}

func TestInvalidInputNoDatabaseLookup(t *testing.T) {
	for _, args := range [][]string{{}, {"-tenant", "not-a-uuid"}, {"-tenant", "11111111-1111-4111-8111-111111111111", "-source", "inbox_members", "-limit", "1001"}, {"-list-sources", "unexpected"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, func(string) string { t.Fatal("unexpected database access"); return "" }, &out, &out); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}

func TestDatabaseErrorDoesNotExposeSecret(t *testing.T) {
	var out bytes.Buffer
	secret := "postgres://user:secret-credential@localhost:invalid/db"
	err := run(context.Background(), []string{"-tenant", "11111111-1111-4111-8111-111111111111", "-source", "inbox_members"}, func(string) string { return secret }, &out, &out)
	if err == nil || strings.Contains(err.Error(), "secret-credential") || out.Len() != 0 {
		t.Fatalf("unsafe failure: %v %q", err, out.String())
	}
}
