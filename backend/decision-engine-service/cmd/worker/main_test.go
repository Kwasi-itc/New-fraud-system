package main

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/app"
	"testing"
)

func TestOutboxQueuePausedWithoutPublisherOtherQueuesPreserved(t *testing.T) {
	cfg := app.Config{ScheduledExecutionQueueName: "scheduled", AsyncExecutionQueueName: "async", AsyncExecutionCallbackQueueName: "callbacks", WorkflowDispatchQueueName: "workflow", ScreeningDispatchQueueName: "screening", ScoringDispatchQueueName: "scoring", OutboxQueueName: "outbox", OutboxQueueWorkers: 4}
	for _, enabled := range []bool{false, true} {
		cfg.OutboxPublisherURL = ""
		if enabled {
			cfg.OutboxPublisherURL = "http://receiver/events"
		}
		queues := executionQueues(cfg)
		_, exists := queues["outbox"]
		if exists != enabled {
			t.Fatal("incorrect outbox consumer policy")
		}
		for _, name := range []string{"scheduled", "async", "callbacks", "workflow", "screening", "scoring"} {
			if _, ok := queues[name]; !ok {
				t.Fatalf("lost queue %s", name)
			}
		}
	}
}

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
