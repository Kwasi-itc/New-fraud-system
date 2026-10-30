package postgres_test

import (
	"context"
	"os"
	"testing"
)

func TestUnusedObjectExclusionMigrationRoundTrip(t *testing.T) {
	db := deliveryDatabase(t)
	for _, direction := range []string{"down", "up"} {
		raw, err := os.ReadFile("../../migrations/metadata/000009_object_exclusions." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(context.Background(), string(raw)); err != nil {
			t.Fatal(direction, err)
		}
	}
}
