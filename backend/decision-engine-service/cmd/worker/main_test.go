package main

import "testing"

func TestToTaskSetDeduplicatesTasks(t *testing.T) {
	taskSet := toTaskSet([]string{"scheduled", "async", "scheduled"})

	if len(taskSet) != 2 {
		t.Fatalf("len(taskSet) = %d, want 2", len(taskSet))
	}
	if _, ok := taskSet["scheduled"]; !ok {
		t.Fatal("scheduled task missing")
	}
	if _, ok := taskSet["async"]; !ok {
		t.Fatal("async task missing")
	}
}

func TestWorkerRunnerEnabled(t *testing.T) {
	runner := workerRunner{tasks: toTaskSet([]string{"outbox"})}

	if !runner.enabled("outbox") {
		t.Fatal("enabled(outbox) = false, want true")
	}
	if runner.enabled("scheduled") {
		t.Fatal("enabled(scheduled) = true, want false")
	}
}
