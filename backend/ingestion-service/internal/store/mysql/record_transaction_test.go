package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestPhysicalTransactionReceipt(t *testing.T) {
	for _, scenario := range []string{"replay", "owner_rollback", "receipt_rollback", "lost_commit"} {
		t.Run(scenario, func(t *testing.T) {
			f := recordFixture(t)
			installEffects(t, f.db, f.database)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var observed atomic.Bool
			pool := f.db
			if scenario == "lost_commit" {
				network := "record_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
					conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
					if err != nil {
						return nil, err
					}
					return &receiptCommitLoss{Conn: conn, observed: &observed}, nil
				})
				defer driver.DeregisterDialContext(network)
				cfg := driver.NewConfig()
				cfg.Net = network
				cfg.Addr = os.Getenv("INGESTION_MYSQL_TEST_ADDRESS")
				cfg.DBName = f.database
				cfg.User = "root"
				cfg.Passwd = "isolated-test-root-only"
				cfg.Timeout = 5 * time.Second
				cfg.ReadTimeout = 10 * time.Second
				cfg.WriteTimeout = 10 * time.Second
				connector, err := driver.NewConnector(cfg)
				if err != nil {
					t.Fatal(err)
				}
				pool = sql.OpenDB(connector)
				defer pool.Close()
			}
			makeManager := func(pool *sql.DB) *ReceiptManager {
				m, err := NewReceiptManager(pool, f.database, f.model.TenantID, ReceiptLimits{1024, 4096, 8}, func(context.Context, ReceiptRequest) error { return nil }, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			command := ingestion.ReceiptCommand{TenantID: f.model.TenantID, TableID: f.model.Tables["records"].ID, Mode: ingestion.ModePatch, Kind: "single", Records: []map[string]any{{"object_id": "atomic"}}}
			request, err := NewReceiptRequest(command, "atomic-receipt", 4096)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			effect := func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
				calls++
				writer, err := NewRecordWriter(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version)
				if err != nil {
					return nil, err
				}
				audit, event := effectRows(f.model.TenantID)
				audit.ObjectID = "atomic"
				event.AggregateKey = "atomic"
				written, err := writer.WriteBatch(ctx, command.Records, command.Mode, audit.CreatedAt, 4096)
				if err != nil {
					return nil, err
				}
				store, err := NewEffectStore(tx, f.database, f.model.TenantID, command.TableID, "records", 8192)
				if err != nil {
					return nil, err
				}
				if err := store.RecordIngestion(ctx, audit, event); err != nil {
					return nil, err
				}
				if scenario == "owner_rollback" {
					return nil, errors.New("owner rejected effects")
				}
				if scenario == "receipt_rollback" {
					return json.RawMessage(`invalid`), nil
				}
				return json.Marshal(ingestion.RecordResult{ObjectID: "atomic", Action: written[0].Action, RevisionID: f.version.String()})
			}
			result, err := makeManager(pool).Execute(ctx, request, effect)
			want := 1
			switch scenario {
			case "owner_rollback", "receipt_rollback":
				want = 0
				if err == nil || len(result.Payload) != 0 {
					t.Fatal("partial result", result, err)
				}
			case "lost_commit":
				var ambiguous *mysqlstore.CommitError
				if !observed.Load() || !errors.As(err, &ambiguous) || len(result.Payload) != 0 {
					t.Fatal("missing real commit fault", result, err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			if want == 1 {
				replay, err := makeManager(f.db).Execute(ctx, request, effect)
				if err != nil || !replay.Replayed || calls != 1 {
					t.Fatal("duplicate physical effects", replay, err, calls)
				}
			}
			for _, object := range []string{f.qualified, "ingestion_audit", "ingestion_outbox_events", "ingestion_request_receipts", "data_model_exact_keys"} {
				var count int
				if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+object).Scan(&count); err != nil || count != want {
					t.Fatal(object, count, want, err)
				}
			}
		})
	}
}

func TestPhysicalLookupForcedCollision(t *testing.T) {
	f := recordFixtureDigest(t, true)
	ctx := context.Background()
	first, err := f.write(ctx, []map[string]any{{"object_id": "collision-A"}, {"object_id": "collision-B"}})
	if err != nil || len(first) != 2 || first[0].ID == first[1].ID {
		t.Fatal(first, err)
	}
	second, err := f.write(ctx, []map[string]any{{"object_id": "collision-B"}, {"object_id": "collision-A"}})
	if err != nil || second[0].ID != first[1].ID || second[1].ID != first[0].ID {
		t.Fatal(second, err)
	}
	if err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		_, found, err := FindRecordKey(ctx, tx, f.database, uuid.New(), f.model.Tables["records"].ID, "collision-A")
		if err != nil {
			return err
		}
		if found {
			return errors.New("cross-tenant key exposed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var buckets int
	if err := f.db.QueryRow("SELECT COUNT(DISTINCT bucket_hash) FROM data_model_exact_keys").Scan(&buckets); err != nil || buckets != 1 {
		t.Fatal(buckets, err)
	}
}
