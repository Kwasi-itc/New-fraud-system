package app

import "testing"

func TestLoadConfigDisablesEventAggregateFacts(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://localhost:8080")
	t.Setenv("EVENT_AGGREGATE_FACTS_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.EventAggregateFactsEnabled {
		t.Fatal("EventAggregateFactsEnabled = true, want false")
	}
	if !cfg.EventStoreConfig().DisableAggregateFacts {
		t.Fatal("event-store aggregate facts were not disabled")
	}
}
