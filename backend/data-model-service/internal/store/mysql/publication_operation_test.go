package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func testPublicationOperations(t *testing.T, db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, err := NewPublicationStore(db, "data_model_publication_test", 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	seed := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		tenantID, tableID := uuid.New(), uuid.New()
		if _, err := db.ExecContext(ctx, "INSERT INTO data_model_tables(id,tenant_id,name) VALUES (?,?,?)", mysqlstore.UUID(tableID), mysqlstore.UUID(tenantID), "records"); err != nil {
			t.Fatal(err)
		}
		return tenantID, tableID
	}
	op := func(request string) PublicationOperation {
		return PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte(request)), Condition: PublicationCondition{Mode: PublicationAnyVersion}}
	}
	noMutation := func(*sql.Tx) error {
		t.Error("receipt replay invoked mutation")
		return errors.New("unexpected mutation")
	}
	t.Run("version_conditions_and_replay", func(t *testing.T) {
		tenantID, tableID := seed()
		create := op("v1:first-publication")
		create.Condition = PublicationCondition{Mode: PublicationMustBeAbsent}
		first, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, create, func(*sql.Tx) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		absent := op("v1:another-first-publication")
		absent.Condition = create.Condition
		if p, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, absent, noMutation); !errors.Is(err, ErrPublicationVersionConflict) || p.Payload != nil {
			t.Fatal("existing publication accepted absent condition", err)
		}
		update := op("v1:conditional-archive")
		update.Condition = PublicationCondition{Mode: PublicationExactVersion, Version: first.Version}
		second, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, update, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=true WHERE id=?", mysqlstore.UUID(tableID))
			return err
		})
		if err != nil || second.Version == first.Version {
			t.Fatal("exact version failed", err)
		}
		stale := op("v1:stale-edit")
		stale.Condition = update.Condition
		if _, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, stale, noMutation); !errors.Is(err, ErrPublicationVersionConflict) {
			t.Fatal("stale version accepted", err)
		}
		if _, err := store.RecoverPublication(ctx, tenantID, tableID, stale); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("stale attempt produced receipt", err)
		}
		for _, original := range []PublicationOperation{create, update} {
			if _, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, original, noMutation); err != nil {
				t.Fatal("valid replay rejected by current version", err)
			}
		}
		altered := update
		altered.Condition = PublicationCondition{Mode: PublicationAnyVersion}
		if _, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, altered, noMutation); !errors.Is(err, ErrPublicationOperationConflict) {
			t.Fatal("condition changed while retaining fingerprint", err)
		}
		if _, err := store.RecoverPublication(ctx, tenantID, tableID, altered); !errors.Is(err, ErrPublicationOperationConflict) {
			t.Fatal("recovery accepted changed condition", err)
		}
		emptyTenant, emptyTable := seed()
		if _, err := store.MutateAndPublishOperation(ctx, emptyTenant, emptyTable, update, noMutation); !errors.Is(err, ErrPublicationVersionConflict) {
			t.Fatal("missing publication matched exact token", err)
		}
	})
	t.Run("concurrent_exact_version_has_one_winner", func(t *testing.T) {
		tenantID, tableID := seed()
		base, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, op("v1:base"), func(*sql.Tx) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		done := make(chan error, 2)
		var calls atomic.Int32
		for _, name := range []string{"first_name", "second_name"} {
			go func(name string) {
				request := op("v1:rename:" + name)
				request.Condition = PublicationCondition{Mode: PublicationExactVersion, Version: base.Version}
				<-start
				_, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, request, func(tx *sql.Tx) error {
					calls.Add(1)
					_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET name=? WHERE id=?", name, mysqlstore.UUID(tableID))
					return err
				})
				done <- err
			}(name)
		}
		close(start)
		conflicts, successes := 0, 0
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err == nil {
					successes++
				} else if errors.Is(err, ErrPublicationVersionConflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if successes != 1 || conflicts != 1 || calls.Load() != 1 {
			t.Fatal("stale callback executed", successes, conflicts, calls.Load())
		}
	})
	t.Run("replay_original_result_conflict_and_tenant", func(t *testing.T) {
		tenantID, tableID := seed()
		operation := op("v1:archive")
		first, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, operation, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=true WHERE id=?", mysqlstore.UUID(tableID))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		later, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, op("v1:restore"), func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=false WHERE id=?", mysqlstore.UUID(tableID))
			return err
		})
		if err != nil || later.Version == first.Version {
			t.Fatal("later publication missing", err)
		}
		for _, recover := range []bool{false, true} {
			var got contract.Publication
			if recover {
				got, err = store.RecoverPublication(ctx, tenantID, tableID, operation)
			} else {
				got, err = store.MutateAndPublishOperation(ctx, tenantID, tableID, operation, noMutation)
			}
			if err != nil || got.Version != first.Version {
				t.Fatal("operation result not retained", err)
			}
			var payload struct {
				Archived bool `json:"archived"`
			}
			if err := json.Unmarshal(got.Payload, &payload); err != nil || !payload.Archived {
				t.Fatal("recovered latest instead of original payload", err)
			}
		}
		conflict := operation
		conflict.RequestHash = sha256.Sum256([]byte("different"))
		if _, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, conflict, noMutation); !errors.Is(err, ErrPublicationOperationConflict) {
			t.Fatal("operation conflict missing", err)
		}
		if _, err := store.RecoverPublication(ctx, tenantID, tableID, conflict); !errors.Is(err, ErrPublicationOperationConflict) {
			t.Fatal("recovery conflict missing", err)
		}
		if _, err := store.RecoverPublication(ctx, uuid.New(), tableID, operation); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("tenant isolation failed", err)
		}
	})
	t.Run("rollback_and_receipt_failure", func(t *testing.T) {
		tenantID, tableID := seed()
		operation := op("v1:rollback")
		failure := errors.New("mutation rejected")
		_, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, operation, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=true WHERE id=?", mysqlstore.UUID(tableID)); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if _, err := store.RecoverPublication(ctx, tenantID, tableID, operation); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("rolled back receipt exists", err)
		}
		// Receipt rejection must roll back the already-written contract too.
		for i := range operation.RequestHash {
			operation.RequestHash[i] = 0xff
		}
		result, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, operation, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=true WHERE id=?", mysqlstore.UUID(tableID))
			return err
		})
		if err == nil || result.Payload != nil {
			t.Fatal("receipt failure reported success")
		}
		var archived bool
		var count int
		if err := db.QueryRowContext(ctx, "SELECT archived FROM data_model_tables WHERE id=?", mysqlstore.UUID(tableID)).Scan(&archived); err != nil || archived {
			t.Fatal("metadata escaped receipt rollback", err)
		}
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_ingestion_contracts WHERE table_id=?", mysqlstore.UUID(tableID)).Scan(&count); err != nil || count != 0 {
			t.Fatal("publication escaped receipt rollback", err)
		}
	})
	t.Run("concurrent_duplicate_operation", func(t *testing.T) {
		tenantID, tableID := seed()
		operation := op("v1:once")
		var calls atomic.Int32
		start := make(chan struct{})
		results := make(chan contract.Publication, 2)
		failures := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				p, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, operation, func(*sql.Tx) error { calls.Add(1); return nil })
				results <- p
				failures <- err
			}()
		}
		close(start)
		var versions []uuid.UUID
		for i := 0; i < 2; i++ {
			select {
			case err := <-failures:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			versions = append(versions, (<-results).Version)
		}
		if calls.Load() != 1 || versions[0] != versions[1] {
			t.Fatal("duplicate mutation executed", calls.Load(), versions)
		}
	})
	t.Run("concurrent_enum_edits", func(t *testing.T) {
		tenantID, tableID := seed()
		fieldID := uuid.New()
		if _, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, op("v1:create-enum"), func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,is_enum) VALUES (?,?,?,?,?,true)", mysqlstore.UUID(fieldID), mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), "state", "string")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		done := make(chan error, 2)
		for _, value := range []string{"a", "b"} {
			go func(value string) {
				<-start
				_, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, op("v1:add-enum:"+value), func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,sort_order) VALUES (?,?,?,?,?,0)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenantID), mysqlstore.UUID(fieldID), value, value)
					return err
				})
				done <- err
			}(value)
		}
		close(start)
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		var payload []byte
		if err := db.QueryRowContext(ctx, "SELECT payload FROM data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Fields map[string]struct {
				Values []struct {
					Value string `json:"value"`
				} `json:"enum_values"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(payload, &doc); err != nil || len(doc.Fields["state"].Values) != 2 {
			t.Fatal("concurrent enum update lost", err)
		}
		seen := map[string]bool{}
		for _, v := range doc.Fields["state"].Values {
			seen[v.Value] = true
		}
		if !seen["a"] || !seen["b"] {
			t.Fatal("wrong enum values published")
		}
	})
	t.Run("lost_commit_acknowledgement", func(t *testing.T) {
		tenantID, tableID := seed()
		operation := op("v1:uncertain")
		var observed atomic.Bool
		faultDB := lostCommitDB(t, &observed)
		faultStore, err := NewPublicationStore(faultDB, "data_model_publication_test", 100, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		p, err := faultStore.MutateAndPublishOperation(ctx, tenantID, tableID, operation, func(tx *sql.Tx) error {
			calls++
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=true WHERE id=?", mysqlstore.UUID(tableID))
			return err
		})
		var uncertain *mysqlstore.CommitError
		if !errors.As(err, &uncertain) || !observed.Load() || calls != 1 || p.Payload != nil {
			t.Fatalf("lost acknowledgement misreported: %v observed=%v calls=%d", err, observed.Load(), calls)
		}
		recovered, err := store.RecoverPublication(ctx, tenantID, tableID, operation)
		if err != nil || recovered.Version == uuid.Nil {
			t.Fatal("durable receipt missing", err)
		}
		replayed, err := store.MutateAndPublishOperation(ctx, tenantID, tableID, operation, noMutation)
		if err != nil || replayed.Version != recovered.Version {
			t.Fatal("uncertain operation not replayed", err)
		}
		var actual mysqlstore.UUID
		if err := db.QueryRowContext(ctx, "SELECT version FROM data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&actual); err != nil || uuid.UUID(actual) != recovered.Version {
			t.Fatal("receipt and contract not atomic", err)
		}
	})
}

func TestPublicationConditionValidation(t *testing.T) {
	for _, condition := range []PublicationCondition{
		{}, {Mode: "unknown"}, {Mode: PublicationExactVersion},
		{Mode: PublicationAnyVersion, Version: uuid.New()},
		{Mode: PublicationMustBeAbsent, Version: uuid.New()},
	} {
		operation := PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte("request")), Condition: condition}
		if err := operation.validate(); err == nil {
			t.Fatal("invalid condition accepted", condition)
		}
	}
	for _, condition := range []PublicationCondition{
		{Mode: PublicationAnyVersion}, {Mode: PublicationMustBeAbsent}, {Mode: PublicationExactVersion, Version: uuid.New()},
	} {
		if err := condition.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

// Plaintext fixture-only wire fault: consume the real COMMIT OK before hiding
// it, proving the server committed rather than merely simulating a client error.
type lostCommitConn struct {
	net.Conn
	drop     atomic.Bool
	observed *atomic.Bool
}

func (c *lostCommitConn) Write(b []byte) (int, error) {
	if len(b) >= 5 && b[4] == 3 && strings.EqualFold(string(b[5:]), "COMMIT") {
		c.drop.Store(true)
	}
	return c.Conn.Write(b)
}
func (c *lostCommitConn) Read(b []byte) (int, error) {
	if !c.drop.Swap(false) {
		return c.Conn.Read(b)
	}
	var header [4]byte
	if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
		return 0, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n < 1 || n > 1024 {
		return 0, fmt.Errorf("unexpected COMMIT response length %d", n)
	}
	packet := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, packet); err != nil {
		return 0, err
	}
	if packet[0] != 0 {
		return 0, errors.New("COMMIT did not return OK")
	}
	c.observed.Store(true)
	_ = c.Conn.Close()
	return 0, io.ErrUnexpectedEOF
}
func lostCommitDB(t *testing.T, observed *atomic.Bool) *sql.DB {
	t.Helper()
	network := "dmcommit" + strings.ReplaceAll(uuid.NewString(), "-", "")
	driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &lostCommitConn{Conn: conn, observed: observed}, nil
	})
	t.Cleanup(func() { driver.DeregisterDialContext(network) })
	cfg := driver.NewConfig()
	cfg.ParseTime = true
	cfg.Net, cfg.Addr, cfg.DBName = network, "mysql:3306", "data_model_publication_test"
	cfg.User, cfg.Passwd = "migration_test", "isolated-test-only"
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 5*time.Second, 5*time.Second
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { db.Close() })
	return db
}
