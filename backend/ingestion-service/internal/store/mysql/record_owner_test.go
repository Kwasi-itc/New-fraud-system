package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func ownerFixture(t *testing.T) (recordFixtureState, *KeyedRecordOwner, ingestion.ReceiptCommand) {
	t.Helper()
	f := recordFixture(t)
	installEffects(t, f.db, f.database)
	f.model.ManagedSystemFields = []string{"updated_at"}
	o, err := NewKeyedRecordOwner(f.db, f.database, f.model.TenantID, f.model.Tables["records"].ID, "records",
		RecordOwnerLimits{Receipt: ReceiptLimits{1024, 1 << 20, 16}, CommandBytes: 1 << 20, WriteBytes: 1 << 20, EffectRowBytes: 1 << 20, EffectBatchBytes: 4 << 20},
		func(context.Context, ReceiptRequest) error { return nil },
		func(context.Context, *sql.Tx, ReceiptRequest) error { return nil },
		func(context.Context, *sql.Tx, ReceiptRequest) (ingestion.PublishedDataModel, error) {
			return f.model, nil
		},
		func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	c := ingestion.ReceiptCommand{TenantID: f.model.TenantID, TableID: f.model.Tables["records"].ID,
		Mode: ingestion.ModePatch, Kind: "single", Records: []map[string]any{{"object_id": "one", "number": json.Number("7"), "amount": json.Number("1.25")}}}
	return f, o, c
}

func assertOwnerCounts(t *testing.T, f recordFixtureState, records, audits, events, receipts int) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	for table, want := range map[string]int{f.qualified: records, schema + ".ingestion_audit": audits,
		schema + ".ingestion_outbox_events": events, schema + ".ingestion_request_receipts": receipts} {
		var n int
		if err := f.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatalf("%s count=%d want=%d: %v", table, n, want, err)
		}
	}
}

func TestKeyedRecordOwnerAtomicity(t *testing.T) {
	for _, scenario := range []string{"replay", "batch", "outbox_failure", "receipt_failure", "late_unique", "validation", "audit_failure", "revoked", "frozen_input", "concurrent"} {
		t.Run(scenario, func(t *testing.T) {
			f, o, c := ownerFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			schema, _ := mysqlstore.Identifier(f.database)
			switch scenario {
			case "outbox_failure", "audit_failure":
				table := "ingestion_outbox_events"
				if scenario == "audit_failure" {
					table = "ingestion_audit"
					c.Records[0]["number"] = "bad"
				}
				if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".deny_effect BEFORE INSERT ON " + schema + "." + table + " FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected effect failure'"); err != nil {
					t.Fatal(err)
				}
			case "receipt_failure":
				o.receipts.limits.ResultBytes = 2
			case "late_unique":
				c.Kind = "batch"
				c.Records = append(c.Records, map[string]any{"object_id": "two", "number": json.Number("7")})
			case "validation":
				c.Records[0]["number"] = "bad"
			case "batch":
				c.Kind = "batch"
				c.Records = append(c.Records, map[string]any{"object_id": "two", "number": json.Number("8")})
			case "frozen_input":
				o.receipts.authorize = func(context.Context, ReceiptRequest) error { c.Records[0]["object_id"] = "mutated"; return nil }
			}
			if scenario == "concurrent" {
				var wg sync.WaitGroup
				var originals atomic.Int32
				errs := make(chan error, 8)
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						r, e := o.Execute(ctx, c, "same")
						if e == nil && !r.Replayed {
							originals.Add(1)
						}
						errs <- e
					}()
				}
				wg.Wait()
				close(errs)
				for e := range errs {
					if e != nil {
						t.Fatal(e)
					}
				}
				if originals.Load() != 1 {
					t.Fatal(originals.Load())
				}
				assertOwnerCounts(t, f, 1, 1, 1, 1)
				return
			}
			r, err := o.Execute(ctx, c, "key")
			switch scenario {
			case "outbox_failure", "receipt_failure", "late_unique", "audit_failure":
				if err == nil || len(r.Payload) != 0 {
					t.Fatal(r, err)
				}
				if scenario == "late_unique" && !errors.Is(err, ErrRecordConflict) {
					t.Fatal(err)
				}
				if scenario == "audit_failure" {
					var validation *RecordValidationError
					if !errors.As(err, &validation) || !strings.Contains(err.Error(), "injected effect failure") {
						t.Fatal(err)
					}
				}
				assertOwnerCounts(t, f, 0, 0, 0, 0)
			case "validation":
				var validation *RecordValidationError
				if !errors.As(err, &validation) || len(r.Payload) != 0 {
					t.Fatal(r, err)
				}
				assertOwnerCounts(t, f, 0, 1, 0, 0)
			case "batch":
				if err != nil {
					t.Fatal(err)
				}
				var results []ingestion.RecordResult
				if err = json.Unmarshal(r.Payload, &results); err != nil || len(results) != 2 || results[0].ObjectID != "one" || results[1].ObjectID != "two" {
					t.Fatal(results, err)
				}
				assertOwnerCounts(t, f, 2, 2, 3, 1)
			case "frozen_input":
				if err != nil || !strings.Contains(string(r.Payload), `"object_id":"one"`) {
					t.Fatal(string(r.Payload), err)
				}
				assertOwnerCounts(t, f, 1, 1, 1, 1)
			default:
				if err != nil || r.Replayed {
					t.Fatal(r, err)
				}
				// Replay must not load/validate the now changed publication.
				o.load = func(context.Context, *sql.Tx, ReceiptRequest) (ingestion.PublishedDataModel, error) {
					return ingestion.PublishedDataModel{}, errors.New("model changed")
				}
				if scenario == "revoked" {
					denied := errors.New("current permission revoked")
					o.receipts.authorizeTx = func(context.Context, *sql.Tx, ReceiptRequest) error { return denied }
					v, e := o.Execute(ctx, c, "key")
					if !errors.Is(e, denied) || len(v.Payload) != 0 {
						t.Fatal(v, e)
					}
				} else {
					v, e := o.Execute(ctx, c, "key")
					if e != nil || !v.Replayed || string(v.Payload) != string(r.Payload) {
						t.Fatal(v, e)
					}
					c.Records[0]["amount"] = json.Number("2")
					if _, e = o.Execute(ctx, c, "key"); !errors.Is(e, ErrReceiptConflict) {
						t.Fatal(e)
					}
				}
				assertOwnerCounts(t, f, 1, 1, 1, 1)
			}
		})
	}
}

func TestKeyedRecordOwnerScopeAndDuplicateValidation(t *testing.T) {
	f, o, c := ownerFixture(t)
	wrong := c
	wrong.TenantID = uuid.New()
	if r, e := o.Execute(context.Background(), wrong, "key"); e == nil || len(r.Payload) != 0 {
		t.Fatal(r, e)
	}
	c.Kind = "batch"
	c.Records = append(c.Records, map[string]any{"object_id": "one"})
	var validation *RecordValidationError
	if r, e := o.Execute(context.Background(), c, "key"); !errors.As(e, &validation) || len(r.Payload) != 0 {
		t.Fatal(r, e)
	}
	assertOwnerCounts(t, f, 0, 2, 0, 0)
}

func TestKeyedRecordOwnerLostCommit(t *testing.T) {
	f, o, c := ownerFixture(t)
	var observed atomic.Bool
	network := "ingestion_owner_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	o.receipts.db = pool
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := o.Execute(ctx, c, "lost")
	var uncertain *mysqlstore.CommitError
	if !observed.Load() || !errors.As(err, &uncertain) || len(r.Payload) != 0 {
		t.Fatal(r, err)
	}
	assertOwnerCounts(t, f, 1, 1, 1, 1)
	o.receipts.db = f.db
	r, err = o.Execute(ctx, c, "lost")
	if err != nil || !r.Replayed {
		t.Fatal(r, err)
	}
	assertOwnerCounts(t, f, 1, 1, 1, 1)
}

func TestKeyedRecordOwnerRestrictedGrants(t *testing.T) {
	f, o, c := ownerFixture(t)
	pool := restrictedReceiptPool(t, f.db, f.database)
	var login string
	if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	account := "'" + strings.Split(login, "@")[0] + "'@'%'"
	schema, _ := mysqlstore.Identifier(f.database)
	exec := func(q string) {
		t.Helper()
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"data_model_tables", "data_model_fields", "data_model_physical_tables", "data_model_physical_fields"} {
		exec("GRANT SELECT ON " + schema + "." + table + " TO " + account)
	}
	exec("GRANT SELECT,INSERT,UPDATE ON " + f.qualified + " TO " + account)
	exec("GRANT INSERT,SELECT(key_digest),UPDATE(key_digest) ON " + schema + ".ingestion_lookup_guards TO " + account)
	exec("GRANT EXECUTE ON FUNCTION " + schema + ".data_model_find_record_object_key_v1 TO " + account)
	for _, table := range []string{"ingestion_audit", "ingestion_outbox_events"} {
		exec("GRANT INSERT ON " + schema + "." + table + " TO " + account)
	}
	o.receipts.db = pool
	ctx := context.Background()
	if _, err := o.Execute(ctx, c, "ok"); err != nil {
		t.Fatal(err)
	}
	if r, err := o.Execute(ctx, c, "ok"); err != nil || !r.Replayed {
		t.Fatal(r, err)
	}
	for _, q := range []string{"SELECT * FROM " + schema + ".data_model_exact_keys", "UPDATE " + schema + ".ingestion_audit SET status='succeeded'", "DELETE FROM " + schema + ".ingestion_request_receipts", "CREATE TABLE " + schema + ".bypass(id INT)"} {
		if _, err := pool.Exec(q); err == nil {
			t.Fatal("unauthorized SQL succeeded", q)
		}
	}
	exec("REVOKE INSERT ON " + schema + ".ingestion_outbox_events FROM " + account)
	c.Records[0]["amount"] = json.Number("9")
	if r, err := o.Execute(ctx, c, "denied-outbox"); err == nil || len(r.Payload) != 0 {
		t.Fatal(r, err)
	}
	assertOwnerCounts(t, f, 1, 1, 1, 1)
	var amount float64
	if err := f.db.QueryRow("SELECT `" + f.columns["amount"]["value"] + "` FROM " + f.qualified).Scan(&amount); err != nil || amount != 1.25 {
		t.Fatal(amount, err)
	}
}
