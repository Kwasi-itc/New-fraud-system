package display

import (
	"context"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	postgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func displayFixture(t *testing.T) (Store, Settings, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("AUTH_PREFLIGHT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL database")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "display_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 5
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	files, err := filepath.Glob("../migrations/metadata/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("missing migrations", err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(raw))
	}
	tenant, other, table, parent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'A','source_data','active',now(),now()),($2,'B','other_data','active',now(),now())`, tenant, other)
	exec(`CREATE SCHEMA source_data; CREATE SCHEMA other_data;
 CREATE TABLE source_data.customers(object_id text PRIMARY KEY,full_name text,secret text);
 CREATE TABLE source_data.payments(object_id text PRIMARY KEY,reference text,customer_id text,amount double precision,state text,secret text);
 INSERT INTO source_data.customers VALUES('c1','Ada Customer','private');
 INSERT INTO source_data.payments VALUES('p1','PAY-001','c1',12.5,'ok','private'),('p2',NULL,'missing',0,'unknown','private');`)
	exec(`INSERT INTO core.model_tables(id,tenant_id,name,caption_field,created_at,updated_at) VALUES($1,$2,'payments','',now(),now()),($3,$2,'customers','full_name',now(),now())`, table, tenant, parent)
	fields := []struct{ name, kind string }{{"object_id", "string"}, {"reference", "string"}, {"customer_id", "string"}, {"amount", "float"}, {"state", "string"}}
	ids := map[string]uuid.UUID{}
	for _, f := range fields {
		ids[f.name] = uuid.New()
		exec(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,is_unique,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,now(),now())`, ids[f.name], tenant, table, f.name, f.kind, f.name == "object_id")
	}
	parentID, captionID, link := uuid.New(), uuid.New(), uuid.New()
	exec(`UPDATE core.model_fields SET is_enum=true WHERE id=$1`, ids["state"])
	exec(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,is_unique,created_at,updated_at) VALUES($1,$2,$3,'object_id','string',true,now(),now()),($4,$2,$3,'full_name','string',false,now(),now())`, parentID, tenant, parent, captionID)
	exec(`INSERT INTO core.model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES($1,$2,'customer',$3,$4,$5,$6,now())`, link, tenant, parent, parentID, table, ids["customer_id"])
	exec(`INSERT INTO core.field_enum_values(id,tenant_id,field_id,value,label,sort_order,created_at,updated_at) VALUES($1,$2,$3,'ok','Approved',0,now(),now())`, uuid.New(), tenant, ids["state"])
	store := Store{DB: db, Transactions: postgres.NewTransactionManager(db), SetCaption: func(ctx context.Context, ms ports.MutationStore, id uuid.UUID, caption string) error {
		_, err := ms.RawTx().Exec(ctx, `UPDATE core.model_tables SET caption_field=$2 WHERE id=$1`, id, caption)
		return err
	}}
	settings, err := store.Get(ctx, table)
	if err != nil {
		t.Fatal(err)
	}
	return store, settings, other
}
func requestFor(s Settings) SaveRequest {
	in := SaveRequest{Version: &s.Version, Caption: "reference", Display: map[string]FieldDisplay{}}
	for _, f := range s.Fields {
		if f.Name == "amount" || f.Name == "customer_id" || f.Name == "state" {
			in.Displayed = append(in.Displayed, f.ID)
			in.Order = append(in.Order, f.ID)
		}
		if f.Name == "customer_id" {
			link := s.Links[0].ID
			in.Display[f.ID.String()] = FieldDisplay{Label: "Customer", LinkID: &link}
		}
	}
	return in
}
func TestDisplaySaveValidationConcurrencyAndRollback(t *testing.T) {
	store, s, _ := displayFixture(t)
	ctx := context.Background()
	in := requestFor(s)
	bad := in
	bad.Caption = "amount"
	if _, err := store.Save(ctx, s.TableID, bad); err == nil {
		t.Fatal("numeric caption accepted")
	}
	bad = in
	bad.Displayed = append(append([]uuid.UUID{}, in.Displayed...), in.Displayed[0])
	if _, err := store.Save(ctx, s.TableID, bad); err == nil {
		t.Fatal("duplicate field accepted")
	}
	bad = in
	bad.Order = []uuid.UUID{uuid.New()}
	if _, err := store.Save(ctx, s.TableID, bad); err == nil {
		t.Fatal("foreign field accepted")
	}
	bad = in
	bad.Displayed = make([]uuid.UUID, 13)
	if _, err := store.Save(ctx, s.TableID, bad); err == nil {
		t.Fatal("oversize selection accepted")
	}
	bad = in
	id := uuid.New()
	bad.Display = map[string]FieldDisplay{s.Fields[0].ID.String(): {LinkID: &id}}
	if _, err := store.Save(ctx, s.TableID, bad); err == nil {
		t.Fatal("undeclared link accepted")
	}
	failing := store
	failing.SetCaption = func(ctx context.Context, ms ports.MutationStore, id uuid.UUID, caption string) error {
		if err := store.SetCaption(ctx, ms, id, caption); err != nil {
			return err
		}
		return errors.New("index delivery unavailable")
	}
	if _, err := failing.Save(ctx, s.TableID, in); err == nil {
		t.Fatal("partial save returned success")
	}
	after, err := store.Get(ctx, s.TableID)
	if err != nil || after.Caption != "" || after.Version != s.Version {
		t.Fatal("save did not roll back", after, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Save(ctx, s.TableID, in); results <- err }()
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent overwrite", success, conflicts)
	}
	var count int
	if err := store.DB.QueryRow(ctx, `SELECT count(*) FROM core.schema_change_log WHERE operation='update_display_settings'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("audit atomicity", count, err)
	}
}
func TestDisplayRecordsRelationshipsLifecycleAndPlans(t *testing.T) {
	store, s, other := displayFixture(t)
	ctx := context.Background()
	in := requestFor(s)
	saved, err := store.Save(ctx, s.TableID, in)
	if err != nil {
		t.Fatal(err)
	}
	objects := []Object{{"payments", "p1"}, {"payments", "p2"}, {"payments", "absent"}, {"unknown", "p1"}}
	batch, err := store.Read(ctx, s.TenantID, objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Records) != 4 || batch.Records[0].Caption != "PAY-001" || batch.Records[2].Found || batch.Records[3].Found {
		t.Fatal("record/caption mapping", batch)
	}
	var customer, state uuid.UUID
	for _, f := range s.Fields {
		if f.Name == "customer_id" {
			customer = f.ID
		}
		if f.Name == "state" {
			state = f.ID
		}
	}
	if batch.Records[0].Values[customer.String()] != "Ada Customer" || batch.Records[1].Values[customer.String()] != "missing" || batch.Records[0].Values[state.String()] != "Approved" {
		t.Fatal("caption/enum fallback", batch)
	}
	if len(batch.Records[0].Values) != 3 {
		t.Fatal("unconfigured values exposed")
	}
	foreign, err := store.Read(ctx, other, objects)
	if err != nil || foreign.Records[0].Found {
		t.Fatal("cross tenant data", foreign, err)
	}
	if _, err = store.Read(ctx, s.TenantID, make([]Object, 101)); err == nil {
		t.Fatal("unbounded request accepted")
	}
	// Exact generated projection on representative data; no per-row source queries.
	if _, err = store.DB.Exec(ctx, `INSERT INTO source_data.customers SELECT 'c'||n,'Customer '||n,'private' FROM generate_series(2,50000)n;
 INSERT INTO source_data.payments SELECT 'p'||n,'PAY-'||n,'c'||n,n,'ok','private' FROM generate_series(3,50000)n;
 ANALYZE source_data.customers; ANALYZE source_data.payments;`); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 1; i <= 100; i++ {
		ids = append(ids, fmt.Sprintf("p%d", i))
	}
	rows, err := store.DB.Query(ctx, "EXPLAIN (ANALYZE,FORMAT TEXT) "+recordQuery(saved, orderedFields(saved)), ids)
	if err != nil {
		t.Fatal(err)
	}
	plan := ""
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line + "\n"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plan)
	if strings.Contains(plan, "Seq Scan on payments") || !strings.Contains(plan, "payments_pkey") || !strings.Contains(plan, "customers_pkey") {
		t.Fatal("expected indexed bounded lookups", plan)
	}
	if _, err = store.DB.Exec(ctx, `DELETE FROM core.model_links WHERE id=$1`, s.Links[0].ID); err != nil {
		t.Fatal(err)
	}
	after, err := store.Get(ctx, s.TableID)
	if err != nil || after.Display[customer.String()].LinkID != nil {
		t.Fatal("stale link display", err)
	}
	if _, err = store.Save(ctx, s.TableID, in); !errors.Is(err, ErrConflict) {
		t.Fatal("lifecycle did not invalidate version", err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE core.model_fields SET archived=true WHERE id=$1`, customer); err != nil {
		t.Fatal(err)
	}
	after, err = store.Get(ctx, s.TableID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range after.Displayed {
		if id == customer {
			t.Fatal("archived field still displayed")
		}
	}
	if _, err = store.DB.Exec(ctx, `DELETE FROM core.model_fields WHERE table_id=$1 AND name='reference'`, s.TableID); err != nil {
		t.Fatal(err)
	}
	after, err = store.Get(ctx, s.TableID)
	if err != nil || after.Caption != "" {
		t.Fatal("deleted caption not cleared", err)
	}
}
