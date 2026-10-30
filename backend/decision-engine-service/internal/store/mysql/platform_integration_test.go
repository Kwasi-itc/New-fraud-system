package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

type platformFixture struct {
	db       *sql.DB
	database string
	runner   *migrations.Runner
	plan     migrations.Plan
	definer  string
}

func platformTestDB(t *testing.T) platformFixture {
	t.Helper()
	address := os.Getenv("DECISION_MYSQL_TEST_ADDRESS")
	if address == "" {
		if os.Getenv("DECISION_MYSQL_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("required disposable MySQL fixture missing")
		}
		t.Skip("isolated decision MySQL fixture not configured")
	}
	if address != "mysql:3306" {
		t.Fatal("only private Compose hostname mysql:3306 is allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(database string) *sql.DB {
		t.Helper()
		db, err := mysqlstore.Open(ctx, platformConfig(database, "root", "isolated-test-root-only"))
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	admin := open("decision_platform_test")
	t.Cleanup(func() { admin.Close() })
	var version string
	if err := admin.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	want := os.Getenv("DECISION_MYSQL_EXPECT_VERSION")
	if want == "" {
		want = "8.4.8"
	}
	if version != want {
		t.Fatalf("fixture version=%s, declared=%s", version, want)
	}
	t.Logf("MySQL fixture version %s (target acceptance requires pinned 8.4.8)", version)
	create := func(prefix string) (*sql.DB, string) {
		t.Helper()
		name := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
		schema, err := mysqlstore.Identifier(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+schema); err != nil {
				t.Error(err)
			}
		})
		db := open(name)
		t.Cleanup(func() { db.Close() })
		return db, name
	}
	// Observations are learned ONLY on a generated reference DB in this private
	// fixture. They are NOT a frozen production/pinned-version release artifact.
	ref, refName := create("decision_ref_")
	statements, err := metadata.PlatformStatements(refName)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ref.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	definitions := make([]string, len(statements))
	for i, step := range statements {
		if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
		definitions[i], err = migrations.Definition(ctx, conn, refName, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
	}
	db, name := create("decision_target_")
	reference, err := metadata.PlatformStatements(name)
	if err != nil {
		t.Fatal(err)
	}
	for i := range reference {
		reference[i].After = definitions[i]
	}
	plan, err := metadata.PlatformPlan(name, reference)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.New(db, "decision_engine", plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal("idempotent install", err)
	}
	if err := runner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return platformFixture{db: db, database: name, runner: runner}
}

func platformConfig(database, user, password string) mysqlstore.Config {
	return mysqlstore.Config{Address: "mysql:3306", Database: database, User: user, Password: password, AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute}
}

func allowPlatformFixture(_ context.Context, _ *sql.Tx, _ uuid.UUID, _ bool) error { return nil }

func (f platformFixture) store(t *testing.T, tenant uuid.UUID, rows, bytes int) *PlatformStore {
	t.Helper()
	store, err := NewPlatformStore(f.db, f.database, tenant, rows, bytes, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func tagFixture(tenant uuid.UUID, object, tag string) platform.RecordTag {
	return platform.RecordTag{ID: uuid.NewString(), TenantID: tenant.String(), ObjectType: "wallets", ObjectID: object, Tag: tag, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func TestMySQLPlatformRoundTripAndTenantIsolation(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	for _, tenant := range []uuid.UUID{a, b} {
		err := f.store(t, tenant, 100, 1<<20).Mutate(ctx, func(r PlatformRepositories) error {
			tag := tagFixture(tenant, "object", "watch")
			stored, err := r.RecordTags.Create(ctx, tag)
			if err != nil {
				return err
			}
			if stored != tag {
				t.Fatal("tag round trip differs", stored, tag)
			}
			_, err = r.RiskSnapshots.Create(ctx, platform.RiskSnapshot{ID: uuid.NewString(), TenantID: tenant.String(), ObjectType: "wallets", ObjectID: "object", RiskLevel: tenant.String(), CreatedAt: tag.CreatedAt})
			if err != nil {
				return err
			}
			_, err = r.IPFlags.Create(ctx, platform.IPFlag{ID: uuid.NewString(), TenantID: tenant.String(), IPAddress: "2001:0db8::1", Flag: "watch", CreatedAt: tag.CreatedAt})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Equal-time risk ties use the same UUID byte order as PostgreSQL.
	if err := f.store(t, a, 100, 1<<20).Mutate(ctx, func(r PlatformRepositories) error {
		at := time.Now().UTC().Truncate(time.Microsecond)
		for i, id := range []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"} {
			if _, err := r.RiskSnapshots.Create(ctx, platform.RiskSnapshot{ID: id, TenantID: a.String(), ObjectType: "wallets", ObjectID: "equal_time", RiskLevel: []string{"low", "high"}[i], CreatedAt: at}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := f.store(t, a, 100, 1<<20).ReadSnapshot(ctx, func(r PlatformRepositories) error {
		tags, err := r.RecordTags.ListByObject(ctx, a.String(), "wallets", "object")
		if err != nil {
			return err
		}
		if len(tags) != 1 || tags[0].TenantID != a.String() {
			t.Fatal("tenant tag leak", tags)
		}
		risk, err := r.RiskSnapshots.GetByObject(ctx, a.String(), "wallets", "object")
		if err != nil {
			return err
		}
		if risk.RiskLevel != a.String() {
			t.Fatal("tenant risk leak")
		}
		tie, err := r.RiskSnapshots.GetByObject(ctx, a.String(), "wallets", "equal_time")
		if err != nil {
			return err
		}
		if tie.RiskLevel != "high" {
			t.Fatal("risk tie order differs from PostgreSQL")
		}
		flags, err := r.IPFlags.ListByIP(ctx, a.String(), "2001:0db8::1")
		if err != nil {
			return err
		}
		if len(flags) != 1 || flags[0].TenantID != a.String() {
			t.Fatal("tenant flag leak")
		}
		if found, err := r.RecordTags.HasTag(ctx, a.String(), "wallets", "object", "watch"); err != nil || !found {
			t.Fatal("stored tag not found", err)
		}
		if found, err := r.IPFlags.HasFlag(ctx, a.String(), "2001:0db8::1", "watch"); err != nil || !found {
			t.Fatal("stored flag not found", err)
		}
		if _, err := r.RecordTags.HasTag(ctx, b.String(), "wallets", "object", "watch"); !errors.Is(err, ErrTenantScope) {
			t.Fatal("cross tenant tag claim", err)
		}
		if _, err := r.RiskSnapshots.GetByObject(ctx, b.String(), "wallets", "object"); !errors.Is(err, ErrTenantScope) {
			t.Fatal("cross tenant risk claim", err)
		}
		if _, err := r.IPFlags.HasFlag(ctx, b.String(), "2001:0db8::1", "watch"); !errors.Is(err, ErrTenantScope) {
			t.Fatal("cross tenant IP claim", err)
		}
		if _, err := r.RiskSnapshots.GetByObject(ctx, a.String(), "wallets", "absent"); !errors.Is(err, ports.ErrRiskSnapshotNotFound) {
			t.Fatal("missing risk contract", err)
		}
		if found, err := r.IPFlags.HasFlag(ctx, a.String(), "2001:db8::1", "watch"); err != nil || found {
			t.Fatal("IP text was normalized", found, err)
		}
		if _, err := r.RecordTags.Create(ctx, tagFixture(a, "readonly", "watch")); err == nil {
			t.Fatal("snapshot permitted a mutation")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPlatformExactTextAndHashCollisions(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	store := f.store(t, tenant, 100, 1<<20)
	objects := []string{"anchor", "Anchor", "anchor ", "é", "e\u0301", "x' OR 1=1 --", "", strings.Repeat("z", 4096)}
	for _, object := range objects {
		if err := store.Mutate(ctx, func(r PlatformRepositories) error {
			_, err := r.RecordTags.Create(ctx, tagFixture(tenant, object, "watch"))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReadSnapshot(ctx, func(r PlatformRepositories) error {
		for _, object := range objects {
			items, err := r.RecordTags.ListByObject(ctx, tenant.String(), "wallets", object)
			if err != nil {
				return err
			}
			if len(items) != 1 || items[0].ObjectID != object {
				t.Fatal("text equality lost case, space, Unicode or full length", object)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Force all fixture rows into the anchor bucket. This simulates a digest
	// collision without weakening the shipped generated-column definitions.
	if _, err := f.db.ExecContext(ctx, "ALTER TABLE decision_engine_record_tags MODIFY object_id_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2('anchor',256))) STORED"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReadSnapshot(ctx, func(r PlatformRepositories) error {
		items, err := r.RecordTags.ListByObject(ctx, tenant.String(), "wallets", "anchor")
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ObjectID != "anchor" {
			t.Fatal("hash bucket replaced full equality", items)
		}
		for _, tag := range []string{"Watch", "watch ", "watch' OR 1=1 --"} {
			found, err := r.RecordTags.HasTag(ctx, tenant.String(), "wallets", "anchor", tag)
			if err != nil || found {
				t.Fatal("non-exact tag matched", tag, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The deliberate collision fixture is schema drift, not acceptable metadata.
	if err := f.runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrDrift) {
		t.Fatal("generated column drift was accepted", err)
	}
}

func TestMySQLPlatformBudgetsAndInvalidValues(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	large := f.store(t, tenant, 100, 1<<20)
	for i := 0; i < 2; i++ {
		if err := large.Mutate(ctx, func(r PlatformRepositories) error {
			_, err := r.RecordTags.Create(ctx, tagFixture(tenant, "object", "watch"))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, limited := range []*PlatformStore{f.store(t, tenant, 1, 1<<20), f.store(t, tenant, 100, 20)} {
		err := limited.ReadSnapshot(ctx, func(r PlatformRepositories) error {
			items, err := r.RecordTags.ListByObject(ctx, tenant.String(), "wallets", "object")
			if items != nil {
				t.Fatal("partial list returned on budget failure")
			}
			return err
		})
		if !errors.Is(err, ErrProjectionLimit) {
			t.Fatal("list limit was not explicit", err)
		}
	}
	for _, modify := range []func(*platform.RecordTag){
		func(v *platform.RecordTag) { v.ID = "not-uuid" },
		func(v *platform.RecordTag) { v.TenantID = uuid.NewString() },
		func(v *platform.RecordTag) { v.Tag = "\x00" },
		func(v *platform.RecordTag) { v.Tag = string([]byte{0xff}) },
		func(v *platform.RecordTag) { v.CreatedAt = v.CreatedAt.Add(time.Nanosecond) },
		func(v *platform.RecordTag) { v.CreatedAt = time.Date(999, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		v := tagFixture(tenant, "bad", "watch")
		modify(&v)
		if err := large.Mutate(ctx, func(r PlatformRepositories) error { _, err := r.RecordTags.Create(ctx, v); return err }); err == nil {
			t.Fatal("invalid value accepted")
		}
	}
	if err := large.Mutate(ctx, func(r PlatformRepositories) error {
		_, err := r.RiskSnapshots.Create(ctx, platform.RiskSnapshot{ID: uuid.NewString(), TenantID: tenant.String(), ObjectType: "wallets", ObjectID: "oversized", RiskLevel: strings.Repeat("x", 1000), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store(t, tenant, 100, 32).ReadSnapshot(ctx, func(r PlatformRepositories) error {
		v, err := r.RiskSnapshots.GetByObject(ctx, tenant.String(), "wallets", "oversized")
		if v != nil {
			t.Fatal("oversized risk returned a partial value")
		}
		return err
	}); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("oversized risk became a successful no-risk", err)
	}
	// Direct SQL cannot store a PostgreSQL-incompatible NUL either.
	_, err := f.db.ExecContext(ctx, "INSERT INTO decision_engine_ip_flags(id,tenant_id,ip_address,flag,created_at) VALUES(?,?,?, ?,UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), "ip\x00address", "flag")
	if mysqlstore.Classify(err) != mysqlstore.CheckViolation {
		t.Fatal("storage NUL constraint absent", err)
	}
}

func TestMySQLPlatformTransactionsAndSnapshot(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	store := f.store(t, tenant, 100, 1<<20)
	injected := errors.New("after owner write")
	v := tagFixture(tenant, "rollback", "watch")
	if err := store.Mutate(ctx, func(r PlatformRepositories) error {
		if _, err := r.RecordTags.Create(ctx, v); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	checkAbsent := func(object string) {
		t.Helper()
		if err := store.ReadSnapshot(ctx, func(r PlatformRepositories) error {
			found, err := r.RecordTags.HasTag(ctx, tenant.String(), "wallets", object, "watch")
			if found {
				t.Fatal("write survived rollback", object)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	checkAbsent("rollback")
	func() {
		defer func() {
			if recover() != "fixture panic" {
				t.Fatal("panic was swallowed")
			}
		}()
		_ = store.Mutate(ctx, func(r PlatformRepositories) error {
			if _, err := r.RecordTags.Create(ctx, tagFixture(tenant, "panic", "watch")); err != nil {
				t.Fatal(err)
			}
			panic("fixture panic")
		})
	}()
	checkAbsent("panic")
	cancelled, cancel := context.WithCancel(ctx)
	if err := store.Mutate(cancelled, func(r PlatformRepositories) error {
		if _, err := r.RecordTags.Create(cancelled, tagFixture(tenant, "cancel", "watch")); err != nil {
			return err
		}
		cancel()
		return nil
	}); err == nil {
		t.Fatal("cancelled transaction committed")
	}
	checkAbsent("cancel")
	// A database error in a later owner write must also undo the first write.
	if err := store.Mutate(ctx, func(r PlatformRepositories) error {
		if _, err := r.RecordTags.Create(ctx, v); err != nil {
			return err
		}
		_, err := r.RecordTags.Create(ctx, v)
		return err
	}); mysqlstore.Classify(err) != mysqlstore.Duplicate {
		t.Fatal("expected duplicate rollback", err)
	}
	checkAbsent("rollback")
	denied := errors.New("fixture authorization denied")
	blocked, err := NewPlatformStore(f.db, f.database, tenant, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return denied })
	if err != nil {
		t.Fatal(err)
	}
	if err := blocked.Mutate(ctx, func(PlatformRepositories) error { t.Fatal("unauthorized callback ran"); return nil }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if err := blocked.ReadSnapshot(ctx, func(PlatformRepositories) error { t.Fatal("unauthorized read ran"); return nil }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if err := store.ReadSnapshot(ctx, func(r PlatformRepositories) error {
		before, err := r.RecordTags.ListByObject(ctx, tenant.String(), "wallets", "snapshot")
		if err != nil {
			return err
		}
		if err := store.Mutate(ctx, func(w PlatformRepositories) error {
			_, err := w.RecordTags.Create(ctx, tagFixture(tenant, "snapshot", "watch"))
			return err
		}); err != nil {
			return err
		}
		after, err := r.RecordTags.ListByObject(ctx, tenant.String(), "wallets", "snapshot")
		if err != nil {
			return err
		}
		if len(before) != 0 || len(after) != 0 {
			t.Fatal("snapshot changed during concurrent write")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Competing identical IDs: one winner, one explicit conflict, no retry.
	barrier := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	v = tagFixture(tenant, "concurrent", "watch")
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			results <- store.Mutate(ctx, func(r PlatformRepositories) error { _, err := r.RecordTags.Create(ctx, v); return err })
		}()
	}
	close(barrier)
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if mysqlstore.Classify(err) == mysqlstore.Duplicate {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal("concurrent identity conflict lost", ok, conflicts)
	}
}

func TestMySQLPlatformRestrictedGrantsAndOwnership(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	user := "de_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	account := "'" + user + "'@'%'"
	if _, err := f.db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-runtime-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	for _, table := range []string{"decision_engine_record_tags", "decision_engine_risk_snapshots", "decision_engine_ip_flags"} {
		if _, err := f.db.ExecContext(ctx, "GRANT SELECT,INSERT ON "+schema+"."+table+" TO "+account); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := mysqlstore.Open(ctx, platformConfig(f.database, user, "isolated-runtime-only"))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	tenant := uuid.New()
	store, err := NewPlatformStore(runtime, f.database, tenant, 100, 1<<20, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Mutate(ctx, func(r PlatformRepositories) error {
		_, err := r.RecordTags.Create(ctx, tagFixture(tenant, "grants", "watch"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"DELETE FROM decision_engine_record_tags", "UPDATE decision_engine_record_tags SET tag='changed'", "DROP TABLE decision_engine_record_tags", "SELECT * FROM decision_engine_schema_migrations"} {
		if _, err := runtime.ExecContext(ctx, query); err == nil {
			t.Fatal("restricted runtime bypass", query)
		}
	}
	if _, err := f.db.ExecContext(ctx, "CREATE TABLE other_owner_marker(id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := f.runner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecContext(ctx, "SELECT * FROM other_owner_marker"); err == nil {
		t.Fatal("cross-owner read grant leaked")
	}
	if _, err := f.db.ExecContext(ctx, "ALTER TABLE decision_engine_ip_flags ADD COLUMN drift INT"); err != nil {
		t.Fatal(err)
	}
	if err := f.runner.Apply(ctx); !errors.Is(err, migrations.ErrDrift) {
		t.Fatal("migration silently accepted drift", err)
	}
	var n int
	if err := f.db.QueryRowContext(ctx, "SELECT count(*) FROM other_owner_marker").Scan(&n); err != nil {
		t.Fatal("other owner affected", err)
	}
}

func TestMySQLPlatformQueryPlans(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	// 5,000 synthetic rows/table: 2,000 share a hot key, 3,000 are selective.
	for offset := 0; offset < 5000; offset += 100 {
		var args []any
		for i := offset; i < offset+100; i++ {
			object := fmt.Sprintf("object_%d", i)
			if i < 2000 {
				object = "hot"
			}
			args = append(args, mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), object, mysqlstore.DateTime{Time: at.Add(time.Duration(i) * time.Microsecond)})
		}
		values := strings.TrimSuffix(strings.Repeat("(?,?,'wallets',?,'watch',?),", 100), ",")
		for _, table := range []struct{ name, value string }{{"record_tags", "tag"}, {"risk_snapshots", "risk_level"}} {
			if _, err := f.db.ExecContext(ctx, "INSERT INTO decision_engine_"+table.name+"(id,tenant_id,object_type,object_id,"+table.value+",created_at) VALUES "+values, args...); err != nil {
				t.Fatal(err)
			}
		}
		values = strings.TrimSuffix(strings.Repeat("(?,?,?,'watch',?),", 100), ",")
		if _, err := f.db.ExecContext(ctx, "INSERT INTO decision_engine_ip_flags(id,tenant_id,ip_address,flag,created_at) VALUES "+values, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"record_tags", "risk_snapshots", "ip_flags"} {
		if _, err := f.db.ExecContext(ctx, "ANALYZE TABLE decision_engine_"+table); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store(t, tenant, 100, 1<<20).ReadSnapshot(ctx, func(r PlatformRepositories) error {
		for _, object := range []string{"object_4000", "hot"} {
			queries := []struct {
				sql  string
				args []any
			}{
				{r.RecordTags.selectTag() + " WHERE " + objectWhere + " ORDER BY created_at DESC,id DESC LIMIT 101", r.RecordTags.objectArgs("wallets", object)},
				{r.RiskSnapshots.selectRisk() + " WHERE " + objectWhere + " ORDER BY created_at DESC,id DESC LIMIT 1", r.RiskSnapshots.objectArgs("wallets", object)},
				{r.IPFlags.selectFlag() + " WHERE " + ipWhere + " ORDER BY created_at DESC,id DESC LIMIT 101", []any{mysqlstore.UUID(tenant), object, object}},
				{r.RecordTags.hasTagQuery(), append(r.RecordTags.objectArgs("wallets", object), "watch", "watch")},
				{r.IPFlags.hasFlagQuery(), []any{mysqlstore.UUID(tenant), object, object, "watch", "watch"}},
			}
			for _, q := range queries {
				var plan string
				if err := r.RecordTags.tx.QueryRowContext(ctx, "EXPLAIN ANALYZE "+q.sql, q.args...).Scan(&plan); err != nil {
					return err
				}
				t.Logf("key=%s: %s", object, plan)
				if !strings.Contains(strings.ToLower(plan), "index lookup") && !strings.Contains(strings.ToLower(plan), "index range scan") {
					t.Fatal("expected bounded indexed access, inspect plan", plan)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
