package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func installEffects(t *testing.T, db *sql.DB, database string) {
	t.Helper()
	ddl, err := EffectSchema(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range ddl {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func effectRows(tenant uuid.UUID) (ingestion.IngestionAudit, ingestion.OutboxEvent) {
	at := time.Date(2026, 10, 28, 12, 30, 1, 123456000, time.FixedZone("test", 3600))
	key := "request\x00with-trailing "
	a := ingestion.IngestionAudit{ID: uuid.NewString(), TenantID: tenant.String(), ObjectType: "records", ObjectID: "object\x00 ", Mode: ingestion.ModePatch, RevisionID: "revision", Status: "succeeded", Payload: []byte(`{"value":9007199254740993,"label":"\u00e9"}`), ValidationError: []byte(`[]`), IdempotencyKey: &key, CreatedAt: at}
	e := ingestion.OutboxEvent{ID: uuid.NewString(), TenantID: tenant.String(), EventType: "record.updated", AggregateType: a.ObjectType, AggregateKey: a.ObjectID, Payload: a.Payload, Status: "pending", CreatedAt: at}
	return a, e
}

func TestEffectsReceiptAtomicity(t *testing.T) {
	for _, scenario := range []string{"replay", "outbox_failure", "receipt_failure", "owner_failure", "restricted", "grant_denial", "cancel_after_effects", "batch"} {
		t.Run(scenario, func(t *testing.T) {
			db, name := receiptFixture(t)
			installEffects(t, db, name)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			tenant, table := uuid.New(), uuid.New()
			audit, event := effectRows(tenant)
			pool := db
			if scenario == "restricted" || scenario == "grant_denial" {
				pool = restrictedReceiptPool(t, db, name)
				var account string
				if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&account); err != nil {
					t.Fatal(err)
				}
				user := strings.Split(account, "@")[0]
				schema, _ := mysqlstore.Identifier(name)
				for _, object := range []string{"ingestion_audit", "ingestion_outbox_events"} {
					if scenario == "grant_denial" && object == "ingestion_outbox_events" {
						continue
					}
					if _, err := db.Exec("GRANT INSERT ON " + schema + "." + object + " TO '" + user + "'@'%'"); err != nil {
						t.Fatal(err)
					}
					for _, statement := range []string{"DELETE FROM " + object, "UPDATE " + object + " SET tenant_id=tenant_id", "SELECT * FROM " + object} {
						if _, err := pool.Exec(statement); err == nil {
							t.Fatal("excess effect privilege", statement)
						}
					}
				}
			}
			// One connection proves no nested pool acquisition by the effect store.
			pool.SetMaxOpenConns(1)
			m, err := NewReceiptManager(pool, name, tenant, ReceiptLimits{1024, 1024, 8}, func(context.Context, ReceiptRequest) error { return nil }, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			r, err := NewReceiptRequest(ingestion.ReceiptCommand{TenantID: tenant, TableID: table, Mode: ingestion.ModePatch, Kind: "single", Records: []map[string]any{{"object_id": audit.ObjectID}}}, "effects", 4096)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "outbox_failure" {
				if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					s, err := NewEffectStore(tx, name, tenant, table, "records", 8192)
					if err != nil {
						return err
					}
					return s.AppendOutbox(ctx, event)
				}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			ownerFailure := errors.New("owner rejected mutation")
			effects := func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
				calls++
				s, err := NewEffectStore(tx, name, tenant, table, "records", 8192)
				if err != nil {
					return nil, err
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO fixture_effects VALUES (?)", mysqlstore.UUID(uuid.New())); err != nil {
					return nil, err
				}
				if err := s.RecordIngestion(ctx, audit, event); err != nil {
					return nil, err
				}
				if scenario == "batch" {
					batch := event
					batch.ID = uuid.NewString()
					batch.EventType = "batch.ingestion.completed"
					batch.AggregateKey = "records"
					if err := s.AppendOutbox(ctx, batch); err != nil {
						return nil, err
					}
				}
				if scenario == "owner_failure" {
					return nil, ownerFailure
				}
				if scenario == "receipt_failure" {
					return json.RawMessage(`invalid`), nil
				}
				if scenario == "cancel_after_effects" {
					cancel()
				}
				return json.RawMessage(`{"action":"updated"}`), nil
			}
			v, err := m.Execute(ctx, r, effects)
			failed := scenario == "outbox_failure" || scenario == "receipt_failure" || scenario == "owner_failure" || scenario == "grant_denial" || scenario == "cancel_after_effects"
			if failed {
				if err == nil || len(v.Payload) != 0 {
					t.Fatal("partial success", v, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if replay, err := m.Execute(ctx, r, effects); err != nil || !replay.Replayed || calls != 1 {
					t.Fatal(replay, err, calls)
				}
			}
			for _, object := range []string{"fixture_effects", "ingestion_audit", "ingestion_outbox_events", "ingestion_request_receipts"} {
				want := 1
				if failed {
					want = 0
				}
				if object == "ingestion_outbox_events" && scenario == "outbox_failure" {
					want = 1
				}
				if object == "ingestion_outbox_events" && scenario == "batch" {
					want = 2
				}
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM " + object).Scan(&count); err != nil || count != want {
					t.Fatal(object, count, want, err)
				}
			}
			if !failed {
				var storedID, storedKey, number string
				var storedTime mysqlstore.DateTime
				if err := db.QueryRow("SELECT object_id,idempotency_key,JSON_UNQUOTE(JSON_EXTRACT(payload,'$.value')),created_at FROM ingestion_audit").Scan(&storedID, &storedKey, &number, &storedTime); err != nil {
					t.Fatal(err)
				}
				if storedID != audit.ObjectID || storedKey != *audit.IdempotencyKey || number != "9007199254740993" || !storedTime.Time.Equal(audit.CreatedAt) {
					t.Fatal("codec drift", storedID, storedKey, number, storedTime)
				}
			}
		})
	}
}

func TestEffectsValidationBeforeWrites(t *testing.T) {
	db, name := receiptFixture(t)
	installEffects(t, db, name)
	tenant, table := uuid.New(), uuid.New()
	ctx := context.Background()
	for _, scenario := range []string{"tenant", "event_tenant", "table_name", "pair_key", "pair_time", "invalid_json", "utf8", "precision", "row_bytes"} {
		t.Run(scenario, func(t *testing.T) {
			a, e := effectRows(tenant)
			switch scenario {
			case "tenant":
				a.TenantID = uuid.NewString()
			case "event_tenant":
				e.TenantID = uuid.NewString()
			case "table_name":
				e.AggregateType = "foreign"
			case "pair_key":
				e.AggregateKey = "different"
			case "pair_time":
				e.CreatedAt = e.CreatedAt.Add(time.Second)
			case "invalid_json":
				e.Payload = []byte(`[]`)
			case "utf8":
				a.ObjectID = "\xff"
			case "precision":
				a.CreatedAt = a.CreatedAt.Add(time.Nanosecond)
			case "row_bytes":
				a.Payload = []byte(`{"value":"` + strings.Repeat("x", 8192) + `"}`)
			}
			// Deliberately commit after validation rejection: validation must have
			// happened before either INSERT, not depend on caller rollback.
			if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				s, err := NewEffectStore(tx, name, tenant, table, "records", 8192)
				if err != nil {
					return err
				}
				if err := s.RecordIngestion(ctx, a, e); err == nil {
					t.Fatal("invalid pair accepted")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, object := range []string{"ingestion_audit", "ingestion_outbox_events"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + object).Scan(&count); err != nil || count != 0 {
			t.Fatal(object, count, err)
		}
	}
	// Failed-validation audit is a separate path and emits no outbox event.
	a, _ := effectRows(tenant)
	a.Status = "validation_failed"
	a.ObjectID = ""
	a.IdempotencyKey = nil
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		s, err := NewEffectStore(tx, name, tenant, table, "records", 8192)
		if err != nil {
			return err
		}
		return s.AppendAudit(ctx, a)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEffectsBulkWrites(t *testing.T) {
	for _, scenario := range []string{"500_records", "late_validation", "late_duplicate", "byte_limit"} {
		t.Run(scenario, func(t *testing.T) {
			db, name := receiptFixture(t)
			installEffects(t, db, name)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			tenant, table := uuid.New(), uuid.New()
			audits := make([]ingestion.IngestionAudit, 500)
			events := make([]ingestion.OutboxEvent, 500)
			for i := range audits {
				audits[i], events[i] = effectRows(tenant)
			}
			if scenario == "late_validation" {
				events[499].TenantID = uuid.NewString()
			}
			if scenario == "late_duplicate" {
				events[499].ID = events[0].ID
			}
			limit := 1 << 20
			if scenario == "byte_limit" {
				limit = 1024
			}
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				s, err := NewEffectStore(tx, name, tenant, table, "records", 8192)
				if err != nil {
					return err
				}
				return s.RecordBatch(ctx, audits, events, limit)
			})
			want := 0
			if scenario == "500_records" {
				want = 500
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid bulk accepted")
			}
			for _, object := range []string{"ingestion_audit", "ingestion_outbox_events"} {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+object).Scan(&count); err != nil || count != want {
					t.Fatal(object, count, want, err)
				}
			}
		})
	}
}
