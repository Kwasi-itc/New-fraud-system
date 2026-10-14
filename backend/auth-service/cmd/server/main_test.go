package main

import (
	"github.com/google/uuid"
	"testing"
)

func TestParseDefaultTenant(t *testing.T) {
	for _, value := range []string{"garbage", " ", uuid.Nil.String()} {
		if _, err := parseDefaultTenant(value); err == nil {
			t.Fatalf("accepted invalid tenant %q", value)
		}
	}
	if id, err := parseDefaultTenant(""); err != nil || id != uuid.Nil {
		t.Fatal("unset configuration", id, err)
	}
	want := uuid.New()
	if id, err := parseDefaultTenant(want.String()); err != nil || id != want {
		t.Fatal("configured tenant", id, err)
	}
}
