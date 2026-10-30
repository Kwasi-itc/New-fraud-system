package service

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	postgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestMigrationWorkerFailureCharacterization(t *testing.T) {
	for _, kind := range []string{"upload", "deferred"} {
		for _, scenario := range []string{"decode", "validation", "database", "terminal_redelivery", "missing_resource"} {
			t.Run(kind+"_"+scenario, func(t *testing.T) {
				b := newMigrationBaseline(t)
				var run func() error
				var state func() (string, int)
				if kind == "upload" {
					repo := postgres.NewUploadLogRepository(b.db)
					owner := NewUploadLogService(repo, b.service, integrationTransactionManager(t, b.db), b.ids, b.clock, 2)
					payload := []byte("object_id,amount,status\nrecord-1,10.5,pending\n")
					if scenario == "decode" {
						payload = []byte("object_id,amount,status\n\"unclosed")
					}
					if scenario == "validation" {
						payload = []byte("object_id,amount,status\nrecord-1,10.5,invalid\n")
					}
					job, err := owner.Create(b.ctx, b.tenant, "transactions", ingestion.ModeCreate, "input.csv", "text/csv", payload)
					if err != nil {
						t.Fatal(err)
					}
					id := uuid.MustParse(job.ID)
					if scenario == "missing_resource" {
						id = uuid.New()
					}
					run = func() error { return owner.RunLog(b.ctx, id) }
					state = func() (string, int) {
						v, e := repo.GetByID(b.ctx, uuid.MustParse(job.ID))
						if e != nil {
							t.Fatal(e)
						}
						return string(v.Status), v.AttemptCount
					}
				} else {
					repo := postgres.NewDeferredIngestRepository(b.db)
					owner := NewDeferredIngestService(repo, b.service, integrationTransactionManager(t, b.db), b.ids, b.clock, 2)
					payload := b.input().Payload
					if scenario == "validation" {
						payload["status"] = "invalid"
					}
					job, err := owner.Create(b.ctx, b.tenant, "transactions", ingestion.ModeCreate, payload, nil)
					if err != nil {
						t.Fatal(err)
					}
					id := uuid.MustParse(job.ID)
					if scenario == "decode" {
						if _, e := b.db.Exec(b.ctx, "UPDATE core_ingestion.deferred_ingests SET payload='[]'::jsonb WHERE id=$1", id); e != nil {
							t.Fatal(e)
						}
					}
					if scenario == "missing_resource" {
						id = uuid.New()
					}
					run = func() error { return owner.RunDeferredIngest(b.ctx, id) }
					state = func() (string, int) {
						v, e := repo.GetByID(b.ctx, uuid.MustParse(job.ID))
						if e != nil {
							t.Fatal(e)
						}
						return string(v.Status), v.AttemptCount
					}
				}
				if scenario == "database" {
					if _, e := b.db.Exec(b.ctx, "ALTER TABLE "+pgx.Identifier{tenantSchemaName(b.tenant), "transactions"}.Sanitize()+" ADD CHECK (amount < 0)"); e != nil {
						t.Fatal(e)
					}
				}
				err := run()
				status, attempt := state()
				switch scenario {
				case "missing_resource":
					if err == nil || attempt != 0 {
						t.Fatal("missing-resource attempt", err, status, attempt)
					}
				case "validation":
					if err != nil || status != "failed" || attempt != 1 {
						t.Fatal("validation terminal behavior", err, status, attempt)
					}
				case "decode", "database":
					pending := "queued"
					if kind == "upload" {
						pending = "uploaded"
					}
					if err == nil || status != pending || attempt != 1 {
						t.Fatal("retry state", err, status, attempt)
					}
					if e := run(); e != nil {
						t.Fatal("exhausted source job should acknowledge persisted terminal state", e)
					}
					status, attempt = state()
					if status != "failed" || attempt != 2 {
						t.Fatal("exhaustion state", status, attempt)
					}
				case "terminal_redelivery":
					if err != nil || status != "completed" || attempt != 1 {
						t.Fatal(err, status, attempt)
					}
					if e := run(); e != nil {
						t.Fatal(e)
					}
					status, attempt = state()
					if status != "completed" || attempt != 2 {
						t.Fatal("source terminal redelivery changed", status, attempt)
					}
				}
				wantAudits := 0
				wantEvents := 0
				if scenario == "validation" {
					wantAudits = 1
				}
				if scenario == "terminal_redelivery" {
					wantAudits = 2
					wantEvents = 2
					if kind == "upload" {
						wantEvents = 4
					}
				}
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", wantAudits)
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", wantEvents)
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.idempotency_keys", 0)
			})
		}
	}
}

func TestMigrationOwnedSchemaInventory(t *testing.T) {
	b := newMigrationBaseline(t)
	want := map[string][]string{
		"ingestion_audit":  {"id", "tenant_id", "object_type", "object_id", "mode", "revision_id", "status", "payload", "validation_errors", "idempotency_key", "created_at"},
		"outbox_events":    {"id", "tenant_id", "event_type", "aggregate_type", "aggregate_key", "payload", "status", "created_at"},
		"idempotency_keys": {"tenant_id", "key", "request_hash", "response_kind", "response_payload", "created_at"},
		"upload_logs":      {"id", "tenant_id", "object_type", "mode", "filename", "content_type", "status", "total_rows", "successful_rows", "failed_rows", "attempt_count", "error_message", "payload", "requested_at", "started_at", "completed_at"},
		"deferred_ingests": {"id", "tenant_id", "object_type", "mode", "status", "attempt_count", "error_message", "idempotency_key", "payload", "requested_at", "started_at", "completed_at"},
	}
	rows, err := b.db.Query(b.ctx, "SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='core_ingestion' ORDER BY table_name,ordinal_position")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var table, column string
		if e := rows.Scan(&table, &column); e != nil {
			t.Fatal(e)
		}
		i := counts[table]
		if i >= len(want[table]) || want[table][i] != column {
			t.Fatal("unmapped source column", table, column)
		}
		counts[table]++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for table, columns := range want {
		if counts[table] != len(columns) {
			t.Fatal("missing source columns", table, counts[table], len(columns))
		}
	}
}
