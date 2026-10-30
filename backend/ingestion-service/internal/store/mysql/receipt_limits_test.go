package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestReceiptBoundsAndLocalAuthorization(t *testing.T) {
	db, name := receiptFixture(t)
	ctx := context.Background()
	tenant := uuid.New()
	denied := errors.New("local permission revoked")
	revoked := false
	m, err := NewReceiptManager(db, name, tenant, ReceiptLimits{32, 128, 1}, func(context.Context, ReceiptRequest) error { return nil }, func(context.Context, *sql.Tx, ReceiptRequest) error {
		if revoked {
			return denied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m.digest = func([]byte) [32]byte { return [32]byte{1} }
	r := ReceiptRequest{TenantID: tenant, TableID: uuid.New(), Key: "key", Mode: "create", Kind: "single", Hash: sha256.Sum256([]byte("command"))}
	calls := 0
	effect := func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
		calls++
		_, err := tx.ExecContext(ctx, "INSERT INTO fixture_effects VALUES (?)", mysqlstore.UUID(uuid.New()))
		return json.RawMessage(`{"value":"original"}`), err
	}
	first, err := m.Execute(ctx, r, effect)
	if err != nil {
		t.Fatal(err)
	}
	first.Payload[0] = 'x'
	replay, err := m.Execute(ctx, r, effect)
	if err != nil || !replay.Replayed || string(replay.Payload) != `{"value":"original"}` || calls != 1 {
		t.Fatal(replay, err, calls)
	}
	revoked = true
	if v, err := m.Execute(ctx, r, effect); !errors.Is(err, denied) || len(v.Payload) != 0 {
		t.Fatal("local replay authorization", v, err)
	}
	revoked = false
	m.limits.ResultBytes = 1
	if v, err := m.Execute(ctx, r, effect); !errors.Is(err, ErrReceiptLimit) || len(v.Payload) != 0 {
		t.Fatal("bounded retained response", v, err)
	}
	m.limits.ResultBytes = 128
	other := r
	other.Key = "another-key"
	if v, err := m.Execute(ctx, other, effect); !errors.Is(err, ErrReceiptLimit) || len(v.Payload) != 0 || calls != 1 {
		t.Fatal("bucket capacity before effects", v, err, calls)
	}
	if v, err := m.Execute(ctx, r, effect); err != nil || !v.Replayed {
		t.Fatal("full bucket must permit replay", v, err)
	}
}

func TestReceiptInvalidResponseRollback(t *testing.T) {
	for _, payload := range []string{`{"broken":`, "{\"value\":\"\xff\"}"} {
		t.Run("invalid", func(t *testing.T) {
			db, name := receiptFixture(t)
			ctx := context.Background()
			tenant := uuid.New()
			m, err := NewReceiptManager(db, name, tenant, ReceiptLimits{32, 128, 2}, func(context.Context, ReceiptRequest) error { return nil }, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			r := ReceiptRequest{TenantID: tenant, TableID: uuid.New(), Key: "key", Mode: "create", Kind: "single", Hash: sha256.Sum256([]byte("command"))}
			v, err := m.Execute(ctx, r, func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
				_, err := tx.ExecContext(ctx, "INSERT INTO fixture_effects VALUES (?)", mysqlstore.UUID(uuid.New()))
				return json.RawMessage(payload), err
			})
			if err == nil || len(v.Payload) != 0 {
				t.Fatal("invalid response accepted", v, err)
			}
			for _, table := range []string{"fixture_effects", "ingestion_request_receipts", "ingestion_receipt_buckets"} {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatal(table, count, err)
				}
			}
		})
	}
}
