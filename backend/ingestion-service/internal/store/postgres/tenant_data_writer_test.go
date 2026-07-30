package postgres

import (
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestEnsureLogicalBucketTimestampsImmutableRejectsChange(t *testing.T) {
	t.Parallel()

	definitions := []ingestion.LogicalBucketDefinition{{TimestampFieldName: "occurred_at"}}
	err := ensureLogicalBucketTimestampsImmutable(
		definitions,
		map[string]any{"occurred_at": "2026-07-01T10:00:00Z"},
		map[string]any{"occurred_at": "2026-07-02T10:00:00Z"},
	)
	if err == nil {
		t.Fatal("ensureLogicalBucketTimestampsImmutable() error = nil, want immutable timestamp error")
	}
}

func TestEnsureLogicalBucketTimestampsImmutableAllowsSameInstant(t *testing.T) {
	t.Parallel()

	definitions := []ingestion.LogicalBucketDefinition{{TimestampFieldName: "occurred_at"}}
	err := ensureLogicalBucketTimestampsImmutable(
		definitions,
		map[string]any{"occurred_at": "2026-07-01T10:00:00Z"},
		map[string]any{"occurred_at": "2026-07-01T12:00:00+02:00"},
	)
	if err != nil {
		t.Fatalf("ensureLogicalBucketTimestampsImmutable() error = %v", err)
	}
}

func TestLogicalDayStartUsesConfiguredDSTTimezone(t *testing.T) {
	t.Parallel()

	beforeTransition, err := logicalDayStart(
		time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC),
		"America/New_York",
	)
	if err != nil {
		t.Fatal(err)
	}
	afterTransition, err := logicalDayStart(
		time.Date(2026, 3, 8, 8, 30, 0, 0, time.UTC),
		"America/New_York",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)
	if !beforeTransition.Equal(want) || !afterTransition.Equal(want) {
		t.Fatalf("logical starts = %s and %s, want %s", beforeTransition, afterTransition, want)
	}
}
