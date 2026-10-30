package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type migrationRandomIDs struct{}

func (migrationRandomIDs) New() uuid.UUID { return uuid.New() }

type migrationHookTransactions struct {
	ports.TransactionManager
	wrap func(ports.MutationStore) ports.MutationStore
}

func (m migrationHookTransactions) Run(ctx context.Context, effect func(ports.MutationStore) error) error {
	return m.TransactionManager.Run(ctx, func(s ports.MutationStore) error { return effect(m.wrap(s)) })
}

type migrationReceiptStore struct {
	ports.MutationStore
	arrived *atomic.Int32
	ready   chan struct{}
}

func (s migrationReceiptStore) Idempotency() ports.IdempotencyRepository {
	return migrationReceiptBarrier{s.MutationStore.Idempotency(), s.arrived, s.ready}
}

type migrationReceiptBarrier struct {
	ports.IdempotencyRepository
	arrived *atomic.Int32
	ready   chan struct{}
}

func (r migrationReceiptBarrier) Get(ctx context.Context, tenant uuid.UUID, key string) (*ingestion.IdempotencyKey, error) {
	v, err := r.IdempotencyRepository.Get(ctx, tenant, key)
	if err != nil || v != nil {
		return v, err
	}
	if r.arrived.Add(1) == 2 {
		close(r.ready)
	}
	select {
	case <-r.ready:
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestMigrationConcurrentReceiptCharacterization(t *testing.T) {
	for _, different := range []bool{false, true} {
		name := "identical"
		if different {
			name = "different"
		}
		t.Run(name, func(t *testing.T) {
			b := newMigrationBaseline(t)
			var arrived atomic.Int32
			ready := make(chan struct{})
			base := integrationTransactionManager(t, b.db)
			tx := migrationHookTransactions{base, func(s ports.MutationStore) ports.MutationStore { return migrationReceiptStore{s, &arrived, ready} }}
			owner := NewIngestService(stubPublishedModelReader{model: b.model}, tx, nil, migrationRandomIDs{}, b.clock)
			type outcome struct {
				input  IngestInput
				result ingestion.RecordResult
				err    error
			}
			results := make(chan outcome, 2)
			for i := 0; i < 2; i++ {
				in := b.input()
				if different && i == 1 {
					in.Payload["object_id"] = "record-2"
					in.Payload["amount"] = 20.5
				}
				go func() {
					v, validation, err := owner.Ingest(b.ctx, in)
					if len(validation) != 0 {
						err = errors.New("unexpected validation")
					}
					results <- outcome{in, v, err}
				}()
			}
			var winner, loser outcome
			for i := 0; i < 2; i++ {
				got := <-results
				if got.err == nil {
					if winner.result.ObjectID != "" {
						t.Fatal("source race unexpectedly replayed")
					}
					winner = got
				} else {
					loser = got
				}
			}
			var pgerr *pgconn.PgError
			if arrived.Load() != 2 || winner.result.ObjectID == "" || !errors.As(loser.err, &pgerr) || pgerr.Code != "23505" || loser.result.ObjectID != "" {
				t.Fatal("absent-receipt race", arrived.Load(), winner.result, loser.err)
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 1)
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", 1)
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.idempotency_keys", 1)
			var count int
			if err := b.db.QueryRow(b.ctx, "SELECT count(*) FROM "+pgx.Identifier{tenantSchemaName(b.tenant), "transactions"}.Sanitize()).Scan(&count); err != nil || count != 1 {
				t.Fatal("loser data leaked", count, err)
			}
			// Retry using the ordinary owner, without the artificial read barrier.
			v, validation, err := b.service.Ingest(b.ctx, loser.input)
			if different {
				if !errors.Is(err, ErrIdempotencyKeyReused) {
					t.Fatal("different retry", err)
				}
			} else if err != nil || len(validation) != 0 || !v.Replayed || v.ObjectID != winner.result.ObjectID {
				t.Fatal("identical retry", v, validation, err)
			}
		})
	}
}

type migrationCancelStore struct {
	ports.MutationStore
	cancel context.CancelFunc
	wrote  *atomic.Bool
}

func (s migrationCancelStore) RecordIngestion(ctx context.Context, a ingestion.IngestionAudit, e ingestion.OutboxEvent) error {
	if err := s.MutationStore.RecordIngestion(ctx, a, e); err != nil {
		return err
	}
	s.wrote.Store(true)
	s.cancel()
	return nil
}

func TestMigrationCancellationAfterWrites(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			b := newMigrationBaseline(t)
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			var wrote atomic.Bool
			tx := migrationHookTransactions{integrationTransactionManager(t, b.db), func(s ports.MutationStore) ports.MutationStore { return migrationCancelStore{s, cancel, &wrote} }}
			owner := NewIngestService(stubPublishedModelReader{model: b.model}, tx, nil, b.ids, b.clock)
			in := b.input()
			var err error
			if batch {
				v, _, e := owner.BatchIngest(ctx, BatchIngestInput{TenantID: b.tenant, ObjectType: in.ObjectType, Mode: in.Mode, Records: []map[string]any{in.Payload}, IdempotencyKey: in.IdempotencyKey})
				err = e
				if len(v) != 0 {
					t.Fatal("provisional batch escaped")
				}
			} else {
				v, _, e := owner.Ingest(ctx, in)
				err = e
				if v.ObjectID != "" {
					t.Fatal("provisional result escaped")
				}
			}
			if !wrote.Load() || !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation not observed after writes", wrote.Load(), err)
			}
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.ingestion_audit", 0)
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.outbox_events", 0)
			assertCoreIngestionCount(t, b.ctx, b.db, "core_ingestion.idempotency_keys", 0)
			var count int
			if e := b.db.QueryRow(b.ctx, "SELECT count(*) FROM "+pgx.Identifier{tenantSchemaName(b.tenant), "transactions"}.Sanitize()).Scan(&count); e != nil || count != 0 {
				t.Fatal("cancelled data leaked", count, e)
			}
			if result := requireIngest(t, b, in); result.Replayed {
				t.Fatal("cancelled receipt retained")
			}
		})
	}
}
