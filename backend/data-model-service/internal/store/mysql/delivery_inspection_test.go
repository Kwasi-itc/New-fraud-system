package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeliveryInspectionBounds(t *testing.T) {
	tenant := uuid.New()
	r := &DeliveryInspector{tenant: tenant}
	for _, limit := range []int{0, -1, 1001} {
		if _, err := r.Page(context.Background(), nil, limit); err == nil {
			t.Fatal("unbounded page accepted")
		}
	}
	for _, cursor := range []DeliveryInspectionCursor{
		{TenantID: uuid.New(), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ID: uuid.New()},
		{TenantID: tenant, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)},
		{TenantID: tenant, ID: uuid.New()},
		{TenantID: tenant, ID: uuid.New(), CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC)},
	} {
		if _, err := r.Page(context.Background(), &cursor, 10); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
	if _, err := r.Get(context.Background(), uuid.Nil); err == nil {
		t.Fatal("empty identity accepted")
	}
	if _, err := NewDeliveryInspector(nil, "shared_app", tenant); err == nil {
		t.Fatal("missing DB accepted")
	}
}
