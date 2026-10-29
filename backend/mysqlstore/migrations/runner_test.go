package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	address := os.Getenv("MYSQLSTORE_TEST_ADDRESS")
	if address == "" {
		if os.Getenv("MYSQLSTORE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("required integration target absent")
		}
		t.Skip("run isolated compose.test.yaml for real-engine acceptance")
	}
	db, err := mysqlstore.Open(context.Background(), mysqlstore.Config{Address: address, Database: "migration_test", User: "migration_test", Password: os.Getenv("MYSQLSTORE_TEST_PASSWORD"), AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Reference observations are obtained from a unique disposable reference object,
// never from the object being adopted on the installation target.
func fixturePlan(t *testing.T, db *sql.DB, name string) Plan {
	t.Helper()
	ctx := context.Background()
	reference := name + "_reference"
	sqlText := "CREATE TABLE `migration_test`.`" + reference + "` (id INT NOT NULL PRIMARY KEY) ENGINE=InnoDB"
	if _, err := db.ExecContext(ctx, sqlText); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	definition, err := Definition(ctx, conn, "migration_test", "TABLE", reference)
	if err != nil {
		t.Fatal(err)
	}
	return Plan{Database: "migration_test", Steps: []Step{{ID: "create_fixture", Kind: "TABLE", Name: name, SQL: strings.ReplaceAll(sqlText, reference, name), After: strings.ReplaceAll(definition, reference, name)}}}
}

func TestMySQLOwnerLedgersAndRecovery(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	owner := "first_" + suffix
	plan := fixturePlan(t, db, "owned_"+suffix)
	r, err := New(db, owner, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx, nil); !errors.Is(err, ErrLineage) {
		t.Fatalf("missing lineage accepted: %v", err)
	}
	if err := r.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// A second owner in the SAME database has an independent history.
	second, err := New(db, "second_"+suffix, fixturePlan(t, db, "other_"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE `migration_test`.`"+r.ledger+"` SET state='dirty',applied_at=NULL"); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx, nil); !errors.Is(err, ErrLineage) {
		t.Fatalf("dirty lineage accepted: %v", err)
	}
	if err := r.Apply(ctx); err != nil {
		t.Fatal(err)
	} // applied DDL, lost journal acknowledgement: inspect, don't repeat
	if err := second.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Steps = append([]Step(nil), plan.Steps...)
	changed.Steps[0].SQL += " "
	drift, _ := New(db, owner, changed)
	if err := drift.Apply(ctx); !errors.Is(err, ErrLineage) {
		t.Fatalf("checksum drift accepted: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE `migration_test`.`"+plan.Steps[0].Name+"` ADD COLUMN unexpected INT"); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx, nil); !errors.Is(err, ErrDrift) {
		t.Fatalf("shape drift accepted: %v", err)
	}
	if _, err := New(db, "bad`owner", plan); err == nil {
		t.Fatal("unsafe owner accepted")
	}
	// Compatibility: data-model's original ledger definition is unchanged.
	legacy, err := New(db, "data_model", plan)
	if err != nil || legacy.ledgerCreate != ledgerCreate {
		t.Fatal("legacy ledger changed")
	}
	var definition map[string]string
	if err := json.Unmarshal([]byte(plan.Steps[0].After), &definition); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBootstrapFailureStopsConsumers(t *testing.T) {
	db := fixture(t)
	failure := errors.New("missing reviewed routine/grant")
	called := false
	err := Bootstrap(context.Background(), db, "migration_test", []BootstrapPass{
		{Name: "consumer", Database: "migration_test", DependsOn: []string{"owner"}, Apply: func(context.Context) error { called = true; return nil }, Verify: func(context.Context) error { return nil }},
		{Name: "owner", Database: "migration_test", Apply: func(context.Context) error { return nil }, Verify: func(context.Context) error { return failure }},
	})
	if !errors.Is(err, failure) || called {
		t.Fatalf("consumer ran despite failed prerequisite: %v", err)
	}
}

func TestMySQLBootstrapOrderingAndExclusion(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	first, err := New(db, "a_"+suffix, fixturePlan(t, db, "a_table_"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(db, "b_"+suffix, fixturePlan(t, db, "b_table_"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	passes := []BootstrapPass{
		{Name: "consumer", Database: "migration_test", DependsOn: []string{"prerequisite"}, Apply: second.Apply, Verify: func(ctx context.Context) error { return second.Verify(ctx, nil) }},
		{Name: "prerequisite", Database: "migration_test", Apply: first.Apply, Verify: func(ctx context.Context) error { return first.Verify(ctx, nil) }},
	}
	if err := Bootstrap(ctx, db, "migration_test", passes); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(ctx, db, "migration_test", passes); err != nil {
		t.Fatal(err)
	}
	// Keep only a session lock while one deployment pass is active. A competing
	// bootstrap must fail explicitly, without running its callbacks or inferring
	// that a canceled client means the first installer stopped.
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	done := make(chan error, 1)
	go func() {
		done <- Bootstrap(ctx, db, "migration_test", []BootstrapPass{{Name: "hold", Database: "migration_test", Apply: func(context.Context) error { close(entered); <-release; return nil }, Verify: func(context.Context) error { return nil }}})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first bootstrap did not enter")
	}
	called := false
	err = Bootstrap(ctx, db, "migration_test", []BootstrapPass{{Name: "compete", Database: "migration_test", Apply: func(context.Context) error { called = true; return nil }, Verify: func(context.Context) error { return nil }}})
	if !errors.Is(err, mysqlstore.ErrDDLLockBusy) || called {
		t.Errorf("competing bootstrap was not excluded: %v", err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
