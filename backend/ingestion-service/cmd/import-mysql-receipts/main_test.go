package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestImportRejectsUnscopedInvocation(t *testing.T) {
	for _, args := range [][]string{nil, {"-mode", "unexpected"}, {"-mode", "import", "-input", "missing", "-database", "application"}} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatal("unscoped invocation accepted", args)
		}
	}
}
func TestCheckpointReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	first := checkpoint{"snapshot", "digest", "target", 1}
	second := first
	second.Line = 2
	if err := saveCheckpoint(path, first); err != nil {
		t.Fatal(err)
	}
	if err := saveCheckpoint(path, second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatal(err)
	}
	if err := saveCheckpoint(filepath.Join(t.TempDir(), "missing", "checkpoint.json"), second); err == nil {
		t.Fatal("checkpoint failure ignored")
	}
}
