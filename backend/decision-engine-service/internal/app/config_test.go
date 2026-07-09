package app

import "testing"

func setRequiredConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://localhost:8080")
	t.Setenv("INGESTION_SERVICE_URL", "http://localhost:8081")
}

func TestLoadConfigRejectsRuleEvaluationConcurrencyAboveGuardrail(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("RULE_EVALUATION_CONCURRENCY", "65")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want guardrail validation error")
	}
}

func TestLoadConfigRejectsScenarioEvaluationConcurrencyAboveGuardrail(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("SCENARIO_EVALUATION_CONCURRENCY", "33")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want scenario guardrail validation error")
	}
}

func TestLoadConfigUsesAllWorkerTasksByDefault(t *testing.T) {
	setRequiredConfigEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	want := []string{"scheduled", "async", "workflow_dispatch", "screening_dispatch", "scoring_dispatch", "outbox"}
	if len(cfg.WorkerTasks) != len(want) {
		t.Fatalf("len(cfg.WorkerTasks) = %d, want %d", len(cfg.WorkerTasks), len(want))
	}
	for i := range want {
		if cfg.WorkerTasks[i] != want[i] {
			t.Fatalf("cfg.WorkerTasks[%d] = %q, want %q", i, cfg.WorkerTasks[i], want[i])
		}
	}
}

func TestLoadConfigRejectsUnsupportedWorkerTasks(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("WORKER_TASKS", "scheduled,invalid")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want worker task validation error")
	}
}

func TestLoadConfigDeduplicatesWorkerTasks(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("WORKER_TASKS", "scheduled,async,scheduled,outbox")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	want := []string{"scheduled", "async", "outbox"}
	if len(cfg.WorkerTasks) != len(want) {
		t.Fatalf("len(cfg.WorkerTasks) = %d, want %d", len(cfg.WorkerTasks), len(want))
	}
	for i := range want {
		if cfg.WorkerTasks[i] != want[i] {
			t.Fatalf("cfg.WorkerTasks[%d] = %q, want %q", i, cfg.WorkerTasks[i], want[i])
		}
	}
}

func TestLoadConfigRejectsNonPositiveExecutionRetrySettings(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("SCHEDULED_EXECUTION_MAX_ATTEMPTS", "0")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want retry setting validation error")
	}
}

func TestLoadConfigRejectsNegativeLiveDecisionConcurrencyLimit(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("LIVE_DECISION_CONCURRENCY_LIMIT", "-1")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want live decision concurrency validation error")
	}
}

func TestLoadConfigParsesWorkerTaskPriorities(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("WORKER_TASKS", "scheduled,async,outbox")
	t.Setenv("WORKER_TASK_PRIORITIES", "outbox:5,scheduled:20,async:10")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.WorkerTaskPriorities["outbox"] != 5 {
		t.Fatalf("outbox priority = %d, want 5", cfg.WorkerTaskPriorities["outbox"])
	}
	if cfg.WorkerTaskPriorities["async"] != 10 {
		t.Fatalf("async priority = %d, want 10", cfg.WorkerTaskPriorities["async"])
	}
}

func TestSortedWorkerTasksUsesPriorityOrder(t *testing.T) {
	got := SortedWorkerTasks([]string{"scheduled", "async", "outbox"}, map[string]int{
		"scheduled": 20,
		"async":     10,
		"outbox":    30,
	})

	want := []string{"async", "scheduled", "outbox"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
