package service

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/testfixtures"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	postgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests deliberately pin source behavior, including known weaknesses.
// They are not target MySQL acceptance or approval to preserve unsafe behavior.
type migrationBaseline struct {
	ctx     context.Context
	db      *pgxpool.Pool
	tenant  uuid.UUID
	model   ingestion.PublishedDataModel
	service IngestService
	clock   fixedClock
	ids     *fixedIDGenerator
}

func newMigrationBaseline(t *testing.T) migrationBaseline {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	url := integrationDatabaseURL(t)
	db := integrationPool(t, ctx, url)
	t.Cleanup(db.Close)
	resetIntegrationDatabase(t, ctx, db, url)
	id := uuid.New()
	createTenantTable(t, ctx, db, tenantSchemaName(id), "transactions")
	b := migrationBaseline{ctx: ctx, db: db, tenant: id, model: publishedTransactionsModel(id), ids: &fixedIDGenerator{next: integrationUUIDSequence(100)}, clock: fixedClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}}
	b.service = NewIngestService(stubPublishedModelReader{model: b.model}, integrationTransactionManager(t, db), nil, b.ids, b.clock)
	return b
}

func (b migrationBaseline) input() IngestInput {
	key := "migration-characterization"
	return IngestInput{TenantID: b.tenant, ObjectType: "transactions", Mode: ingestion.ModeCreate, IdempotencyKey: &key, Payload: testfixtures.MigrationRecord().Payload()}
}

func requireIngest(t *testing.T, b migrationBaseline, input IngestInput) ingestion.RecordResult {
	t.Helper()
	v, validation, err := b.service.Ingest(b.ctx, input)
	if err != nil || len(validation) != 0 {
		t.Fatal("ingest", v, validation, err)
	}
	return v
}

func TestMigrationReplayCharacterization(t *testing.T) {
	for _, scenario := range []string{"single_normalizes", "batch_raw_hash", "mode_not_bound", "table_not_bound", "validation_before_replay", "response_kind", "changed_hash", "batch_duplicates", "failure_audit_error", "batch_failure_audit_error", "alternate_unique_rollback", "batch_sql_rollback"} {
		t.Run(scenario, func(t *testing.T) {
			b := newMigrationBaseline(t)
			in := b.input()
			switch scenario {
			case "single_normalizes":
				in.Payload["amount"] = "10.50"
				first := requireIngest(t, b, in)
				in.Payload["amount"] = 10.5
				replay := requireIngest(t, b, in)
				if !replay.Replayed || replay.Action != first.Action {
					t.Fatal("normalized replay changed", replay)
				}
			case "batch_raw_hash":
				in.Payload["amount"] = "10.50"
				batch := BatchIngestInput{TenantID: b.tenant, ObjectType: in.ObjectType, Mode: in.Mode, Records: []map[string]any{in.Payload}, IdempotencyKey: in.IdempotencyKey}
				if _, v, e := b.service.BatchIngest(b.ctx, batch); e != nil || len(v) != 0 {
					t.Fatal(v, e)
				}
				in.Payload["amount"] = 10.5
				if _, v, e := b.service.BatchIngest(b.ctx, batch); !errors.Is(e, ErrIdempotencyKeyReused) || len(v) != 0 {
					t.Fatal("raw batch identity changed", v, e)
				}
			case "mode_not_bound":
				first := requireIngest(t, b, in)
				in.Mode = ingestion.ModePatch
				v := requireIngest(t, b, in)
				if !v.Replayed || v.Action != first.Action {
					t.Fatal("source mode replay", v)
				}
			case "table_not_bound":
				first := requireIngest(t, b, in)
				createTenantTable(t, b.ctx, b.db, tenantSchemaName(b.tenant), "other_records")
				other := b.model.Tables["transactions"]
				other.ID = uuid.New()
				other.Name = "other_records"
				b.model.Tables[other.Name] = other
				in.ObjectType = other.Name
				v := requireIngest(t, b, in)
				if !v.Replayed || v.Action != first.Action {
					t.Fatal("source cross-table replay", v)
				}
				var count int
				if e := b.db.QueryRow(b.ctx, "SELECT count(*) FROM "+pgx.Identifier{tenantSchemaName(b.tenant), other.Name}.Sanitize()).Scan(&count); e != nil || count != 0 {
					t.Fatal("unexpected other-table write", count, e)
				}
			case "validation_before_replay":
				requireIngest(t, b, in)
				object := b.model.Tables[in.ObjectType]
				field := object.Fields["status"]
				field.EnumValues = []ingestion.EnumValue{{Value: "approved", Label: "Approved"}}
				object.Fields["status"] = field
				v, validation, e := b.service.Ingest(b.ctx, in)
				if e != nil || len(validation) == 0 || v.Replayed {
					t.Fatal("source replay bypassed validation", v, validation, e)
				}
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 2)
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", 1)
				return
			case "response_kind", "changed_hash":
				requireIngest(t, b, in)
				if scenario == "response_kind" {
					if _, e := b.db.Exec(b.ctx, "UPDATE core_ingestion.idempotency_keys SET response_kind='batch' WHERE tenant_id=$1 AND key=$2", b.tenant, *in.IdempotencyKey); e != nil {
						t.Fatal(e)
					}
				} else {
					in.Payload["amount"] = 99.5
				}
				if _, _, e := b.service.Ingest(b.ctx, in); !errors.Is(e, ErrIdempotencyKeyReused) {
					t.Fatal("receipt conflict", e)
				}
			case "batch_duplicates":
				_, validation, e := b.service.BatchIngest(b.ctx, BatchIngestInput{TenantID: b.tenant, ObjectType: in.ObjectType, Mode: in.Mode, Records: []map[string]any{in.Payload, in.Payload}, IdempotencyKey: in.IdempotencyKey})
				if e != nil || len(validation) != 1 || validation[0].Code != "duplicate_object_id" {
					t.Fatal(validation, e)
				}
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 2)
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", 0)
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.idempotency_keys", 0)
				return
			case "failure_audit_error", "batch_failure_audit_error":
				if _, e := b.db.Exec(b.ctx, "ALTER TABLE core_ingestion.ingestion_audit ADD CONSTRAINT reject_failure CHECK (status <> 'validation_failed')"); e != nil {
					t.Fatal(e)
				}
				in.Payload["status"] = "invalid"
				if scenario == "failure_audit_error" {
					_, validation, e := b.service.Ingest(b.ctx, in)
					if e != nil || len(validation) == 0 {
						t.Fatal("source audit error handling", validation, e)
					}
				} else {
					_, validation, e := b.service.BatchIngest(b.ctx, BatchIngestInput{TenantID: b.tenant, ObjectType: in.ObjectType, Mode: in.Mode, Records: []map[string]any{in.Payload}})
					if e != nil || len(validation) == 0 {
						t.Fatal("source batch audit error handling", validation, e)
					}
				}
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 0)
				assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", 0)
				return
			case "alternate_unique_rollback", "batch_sql_rollback":
				table := pgx.Identifier{tenantSchemaName(b.tenant), "transactions"}.Sanitize()
				if _, e := b.db.Exec(b.ctx, "ALTER TABLE "+table+" ADD UNIQUE (amount)"); e != nil {
					t.Fatal(e)
				}
				if scenario == "alternate_unique_rollback" {
					requireIngest(t, b, in)
					in.IdempotencyKey = nil
					in.Payload["object_id"] = "other"
					if _, _, e := b.service.Ingest(b.ctx, in); e == nil {
						t.Fatal("alternate uniqueness redirected write")
					}
				} else {
					_, _, e := b.service.BatchIngest(b.ctx, BatchIngestInput{TenantID: b.tenant, ObjectType: in.ObjectType, Mode: in.Mode, Records: []map[string]any{in.Payload, {"object_id": "other", "amount": 10.5, "status": "pending"}}, IdempotencyKey: in.IdempotencyKey})
					if e == nil {
						t.Fatal("batch uniqueness accepted")
					}
					assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 0)
					assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", 0)
					assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.idempotency_keys", 0)
					var count int
					if e := b.db.QueryRow(b.ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 0 {
						t.Fatal("partial batch", count, e)
					}
					return
				}
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 1)
			wantEvents := 1
			if scenario == "batch_raw_hash" {
				wantEvents = 2
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", wantEvents)
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.idempotency_keys", 1)
		})
	}
}

type interruptedUploadCompletion struct {
	ports.UploadLogRepository
	interrupted bool
}

func (r *interruptedUploadCompletion) Update(ctx context.Context, value ingestion.UploadLog) error {
	if !r.interrupted && value.Status == ingestion.UploadLogStatusCompleted {
		r.interrupted = true
		return errors.New("fixture interrupted after ingest commit")
	}
	return r.UploadLogRepository.Update(ctx, value)
}

type interruptedDeferredCompletion struct {
	ports.DeferredIngestRepository
	interrupted bool
}

func (r *interruptedDeferredCompletion) Update(ctx context.Context, value ingestion.DeferredIngest) error {
	if !r.interrupted && value.Status == ingestion.DeferredIngestStatusCompleted {
		r.interrupted = true
		return errors.New("fixture interrupted after ingest commit")
	}
	return r.DeferredIngestRepository.Update(ctx, value)
}

func TestMigrationWorkerCompletionGap(t *testing.T) {
	for _, kind := range []string{"upload", "deferred_without_key", "deferred_with_key"} {
		t.Run(kind, func(t *testing.T) {
			b := newMigrationBaseline(t)
			var run func() error
			if kind == "upload" {
				repo := postgres.NewUploadLogRepository(b.db)
				fault := &interruptedUploadCompletion{UploadLogRepository: repo}
				owner := NewUploadLogService(fault, b.service, integrationTransactionManager(t, b.db), b.ids, b.clock, 3)
				job, e := owner.Create(b.ctx, b.tenant, "transactions", ingestion.ModeCreate, "records.csv", "text/csv", testfixtures.MigrationRecord().CSV())
				if e != nil {
					t.Fatal(e)
				}
				run = func() error { return owner.RunLog(b.ctx, uuid.MustParse(job.ID)) }
			} else {
				repo := postgres.NewDeferredIngestRepository(b.db)
				fault := &interruptedDeferredCompletion{DeferredIngestRepository: repo}
				owner := NewDeferredIngestService(fault, b.service, integrationTransactionManager(t, b.db), b.ids, b.clock, 3)
				var key *string
				if kind == "deferred_with_key" {
					key = b.input().IdempotencyKey
				}
				job, e := owner.Create(b.ctx, b.tenant, "transactions", ingestion.ModeCreate, b.input().Payload, key)
				if e != nil {
					t.Fatal(e)
				}
				run = func() error { return owner.RunDeferredIngest(b.ctx, uuid.MustParse(job.ID)) }
			}
			if e := run(); e == nil {
				t.Fatal("completion interruption missing")
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 1)
			if e := run(); e != nil {
				t.Fatal(e)
			}
			want := 2
			if kind == "deferred_with_key" {
				want = 1
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", want)
			events := want
			if kind == "upload" {
				events *= 2
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", events)
			assertTenantRowValues(t, b.ctx, b.db, tenantSchemaName(b.tenant), "transactions", "record-1", "pending", 10.5)
		})
	}
}
