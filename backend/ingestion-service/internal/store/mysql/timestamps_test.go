package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
)

func TestIngestionWorkAttemptTimestamps(t *testing.T) {
	for _, kind := range []string{UploadWork, DeferredWork} {
		t.Run(kind, func(t *testing.T) {
			f, w := workFixture(t)
			ctx := context.Background()
			in := workInput(kind)
			r, err := w.Create(ctx, in)
			if err != nil || r.StartedAt != nil || r.CompletedAt != nil || r.CreatedAt.IsZero() {
				t.Fatal(r, err)
			}
			created := r.CreatedAt
			l := claimWork(t, w, kind)
			r, err = w.Get(ctx, in.ID)
			if err != nil || r.StartedAt == nil || r.StartedAt.Before(created) || r.Attempt != 1 {
				t.Fatal(r, err)
			}
			// A fixed past attempt makes the renewal/retry distinction deterministic
			// without asserting against a client clock or sleeping for timestamp changes.
			past := time.Date(2020, 1, 1, 0, 0, 0, 123456000, time.UTC)
			if _, err = f.db.Exec("UPDATE "+w.owner.receipts.schema+".ingestion_work_jobs SET started_at=? WHERE id=?", mysqlstore.DateTime{Time: past}, mysqlstore.UUID(in.ID)); err != nil {
				t.Fatal(err)
			}
			if err = w.Queue().Heartbeat(ctx, l, time.Minute); err != nil {
				t.Fatal(err)
			}
			if err = w.Queue().Retry(ctx, l, time.Second, "test_retry"); err != nil {
				t.Fatal(err)
			}
			r, err = w.Get(ctx, in.ID)
			if err != nil || r.StartedAt != nil || r.CompletedAt != nil {
				t.Fatal(r, err)
			}
			var retained mysqlstore.DateTime
			if err := f.db.QueryRow("SELECT started_at FROM "+w.owner.receipts.schema+".ingestion_work_jobs WHERE id=?", mysqlstore.UUID(in.ID)).Scan(&retained); err != nil || !retained.Time.Equal(past) {
				t.Fatal(retained, err)
			}
			if _, err = f.db.Exec("UPDATE "+w.owner.receipts.schema+".ingestion_work_jobs SET available_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(in.ID)); err != nil {
				t.Fatal(err)
			}
			next := claimWork(t, w, kind)
			r, err = w.Get(ctx, in.ID)
			if err != nil || r.StartedAt == nil || !r.StartedAt.After(past) || r.Attempt != 2 {
				t.Fatal(r, err)
			}
			started := *r.StartedAt
			if err = w.Handle(ctx, l); !errors.Is(err, work.ErrLease) {
				t.Fatal(err)
			}
			if err = w.Handle(ctx, next); err != nil {
				t.Fatal(err)
			}
			r, err = w.Get(ctx, in.ID)
			if err != nil || r.StartedAt == nil || !r.StartedAt.Equal(started) || r.CompletedAt == nil || r.CompletedAt.Before(started) || !r.CreatedAt.Equal(created) {
				t.Fatal(r, err)
			}
		})
	}
}

func TestReceiptCreationTimeSurvivesReplay(t *testing.T) {
	f, o, c := ownerFixture(t)
	ctx := context.Background()
	if _, err := o.Execute(ctx, c, "timestamp"); err != nil {
		t.Fatal(err)
	}
	var before, after mysqlstore.DateTime
	query := "SELECT created_at FROM " + o.receipts.receiptTable + " WHERE tenant_id=? AND request_key=?"
	if err := f.db.QueryRow(query, mysqlstore.UUID(o.receipts.tenant), []byte("timestamp")).Scan(&before); err != nil || before.Time.IsZero() {
		t.Fatal(before, err)
	}
	if r, err := o.Execute(ctx, c, "timestamp"); err != nil || !r.Replayed {
		t.Fatal(r, err)
	}
	if err := f.db.QueryRow(query, mysqlstore.UUID(o.receipts.tenant), []byte("timestamp")).Scan(&after); err != nil || !before.Time.Equal(after.Time) {
		t.Fatal(before, after, err)
	}
}
