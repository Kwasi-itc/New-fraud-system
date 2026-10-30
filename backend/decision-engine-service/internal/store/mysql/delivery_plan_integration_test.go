package mysql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// Small synthetic selectivity acceptance, NOT application-scale throughput or
// lock-duration evidence. Callers prepare at least 5,000 transport rows so a
// primary/table scan cannot pass merely because the fixture has one job.
func assertExpiredDeliveryPlan(t *testing.T, db *sql.DB, store *EffectDeliveryStore) {
	t.Helper()
	ctx := context.Background()
	page, err := store.Expired(ctx, nil, 1)
	if err != nil || len(page) != 1 {
		t.Fatal("missing expired plan fixture", page, err)
	}
	expected := "de_effect_delivery_rescue"
	if store.executionKinds {
		expected = "de_execution_delivery_rescue"
	}
	for _, after := range []*ExpiredDeliveryCursor{nil, {LeaseUntil: page[0].LeaseUntil, ID: page[0].ID}} {
		query, args := store.expiredSelect(after, 100)
		var plan string
		if err := db.QueryRowContext(ctx, "EXPLAIN ANALYZE "+query, args...).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		t.Log(plan)
		if !strings.Contains(plan, expected) || strings.Contains(strings.ToLower(plan), "table scan") {
			t.Fatal("expired discovery lost scoped index or scans history", plan)
		}
	}
}
