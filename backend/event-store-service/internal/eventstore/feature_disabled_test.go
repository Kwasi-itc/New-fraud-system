package eventstore

import "testing"

func TestNewRepositoryCanDisableAggregateFacts(t *testing.T) {
	repository, err := NewRepository(Config{
		ClickHouseURL:         "http://127.0.0.1:8123",
		DisableAggregateFacts: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	defer repository.Close()

	if repository.factsEnabled || repository.store.factsEnabled {
		t.Fatal("aggregate facts remain enabled")
	}
	if repository.features != nil {
		t.Fatal("disabled aggregate facts should not create a Valkey feature cache")
	}
}
