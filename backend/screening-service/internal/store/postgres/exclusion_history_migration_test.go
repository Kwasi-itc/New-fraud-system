package postgres_test

import (
	"context"
	"os"
	"testing"
)

func TestExclusionHistoryIndexMigrationRoundTrip(t *testing.T) {
	db := deliveryDatabase(t)
	for _, direction := range []string{"down", "up"} {
		raw, err := os.ReadFile("../../migrations/metadata/000010_exclusion_history_indexes." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(context.Background(), string(raw)); err != nil {
			t.Fatal(direction, err)
		}
		var count int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_indexes WHERE schemaname='screening' AND indexname IN ('investigator_reviews_case_history_idx','object_exclusion_applications_history_idx')`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if direction == "up" {
			want = 2
		}
		if count != want {
			t.Fatal(direction, count)
		}
	}
}
