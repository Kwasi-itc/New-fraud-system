package main

import (
	"context"
	"strings"
	"testing"

	"github.com/riverqueue/river/rivermigrate"
)

func TestSharedRiverRollbackRejectedBeforeConnecting(t *testing.T) {
	err := runRiverMigrations(context.Background(), "not-a-database", rivermigrate.DirectionDown, nil)
	if err == nil || !strings.Contains(err.Error(), "shared River rollback") {
		t.Fatalf("unowned queue rollback was not rejected before connecting: %v", err)
	}
}
