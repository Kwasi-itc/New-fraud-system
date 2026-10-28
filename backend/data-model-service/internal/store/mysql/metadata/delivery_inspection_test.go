package metadata_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestDeliveryInspection(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, b := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	var ids []uuid.UUID
	states := map[uuid.UUID]string{}
	created := map[uuid.UUID]time.Time{}
	var foreign uuid.UUID
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		for _, tenantID := range []uuid.UUID{a, b} {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
			if err != nil {
				return err
			}
			if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "inspection", nil)); err != nil {
				return err
			}
			count := 1003
			// Populate both tenants. A nearly single-tenant table gives the
			// primary and tenant indexes effectively tied costs, so a covering
			// index assertion on that distribution depends on random UUIDs.
			for i := 0; i < count; i++ {
				job, id := uuid.New(), uuid.New()
				createdAt := at
				if i >= 1000 {
					createdAt = at.Add(-time.Minute)
				}
				large := tenantID == a && i == 0
				if large {
					id = uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
				}
				state := []string{"available", "running", "completed", "cancelled", "discarded"}[i%5]
				if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_index_jobs(id,tenant_id,table_name,index_type,columns_json,status,requested_at,error_message) VALUES (?,?,IF(?,REPEAT('x',1048576),'records'),'search',IF(?,JSON_ARRAY(REPEAT('private-column',80000)),JSON_ARRAY()),'pending',?,IF(?,REPEAT('private-error',80000),'private-error-not-for-operator-list'))", mysqlstore.UUID(job), mysqlstore.UUID(tenantID), large, large, mysqlstore.DateTime{Time: at}, large); err != nil {
					return err
				}
				var token, until, finalized, retained any
				if state == "running" {
					token, until = mysqlstore.UUID(uuid.New()), mysqlstore.DateTime{Time: at.Add(time.Hour)}
				}
				if state == "completed" || state == "cancelled" || state == "discarded" {
					finalized, retained = mysqlstore.DateTime{Time: at}, mysqlstore.DateTime{Time: at.Add(7 * 24 * time.Hour)}
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_index_deliveries(id,tenant_id,job_id,queue_name,state,available_at,created_at,attempt,max_attempts,lease_token,lease_until,finalized_at,retain_until,last_error_code) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,'fixture_code')", mysqlstore.UUID(id), mysqlstore.UUID(tenantID), mysqlstore.UUID(job), fmt.Sprintf("queue_%d", i%2), state, mysqlstore.DateTime{Time: at}, mysqlstore.DateTime{Time: createdAt}, 1, 1, token, until, finalized, retained); err != nil {
					return err
				}
				if state == "available" || state == "running" || state == "completed" {
					if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_index_delivery_reservations(tenant_id,job_id,delivery_id) VALUES (?,?,?)", mysqlstore.UUID(tenantID), mysqlstore.UUID(job), mysqlstore.UUID(id)); err != nil {
						return err
					}
				}
				if tenantID == a {
					ids = append(ids, id)
					states[id] = state
					created[id] = createdAt
				} else {
					foreign = id
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Slice(ids, func(i, j int) bool {
		if !created[ids[i]].Equal(created[ids[j]]) {
			return created[ids[i]].After(created[ids[j]])
		}
		return bytes.Compare(ids[i][:], ids[j][:]) > 0
	})
	r, err := store.NewDeliveryInspector(db, database, a)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	selects := func() int {
		var name string
		var n int
		if err := db.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, size := range []int{1, 1000} {
		before := selects()
		page, err := r.Page(ctx, nil, size)
		if err != nil || len(page.Items) != size || page.Next == nil {
			t.Fatalf("page %d: %d %v", size, len(page.Items), err)
		}
		if used := selects() - before; used != 1 {
			t.Fatalf("page %d issued %d SELECTs", size, used)
		}
		for i, item := range page.Items {
			if item.ID != ids[i] {
				t.Fatal("unstable equal-time order")
			}
		}
		encoded, err := json.Marshal(page)
		if err != nil || bytes.Contains(encoded, []byte("lease_token")) || bytes.Contains(encoded, []byte("private-error")) || bytes.Contains(encoded, []byte("columns_json")) {
			t.Fatalf("unsafe projection %v", err)
		}
	}
	var seen []uuid.UUID
	var cursor *store.DeliveryInspectionCursor
	for {
		page, err := r.Page(ctx, cursor, 127)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			seen = append(seen, item.ID)
			state := states[item.ID]
			reserved := state == "available" || state == "running" || state == "completed"
			if item.State != state || item.JobStatus != "pending" || item.Attempt != 1 || item.JobAttempt != 0 || item.MaxAttempts != 1 || item.Reserved != reserved || (item.LeaseUntil != nil) != (state == "running") || item.LastErrorCode != "fixture_code" {
				t.Fatalf("projection mismatch: %+v", item)
			}
		}
		if page.Next == nil {
			break
		}
		cursor = page.Next
	}
	if len(seen) != len(ids) {
		t.Fatalf("lost rows %d/%d", len(seen), len(ids))
	}
	for i := range ids {
		if ids[i] != seen[i] {
			t.Fatal("paging duplicate or omission")
		}
	}
	if _, err := r.Get(ctx, foreign); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign delivery exposed", err)
	}
	if _, err := r.Get(ctx, uuid.New()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing delivery hidden", err)
	}
	one, err := r.Get(ctx, ids[0])
	if err != nil || one.ID != ids[0] {
		t.Fatalf("detail: %+v %v", one, err)
	}
	other, err := store.NewDeliveryInspector(db, database, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Page(ctx, cursor, 1); err == nil {
		t.Fatal("foreign cursor accepted")
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if page, err := r.Page(cancelled, nil, 10); !errors.Is(err, context.Canceled) || len(page.Items) != 0 {
		t.Fatalf("cancelled partial result: %+v %v", page, err)
	}
	// Explain the bounded keyset selection with representative equal-time data.
	// The index covers the tenant and ordering; this is not a load benchmark.
	// The fixture bulk-loads a new table. Refresh optimizer statistics before
	// asserting its plan rather than racing InnoDB's asynchronous stats refresh.
	var analyzedTable, analyzedOperation, analyzedType, analyzedMessage string
	if err := db.QueryRowContext(ctx, "ANALYZE TABLE data_model_index_deliveries").Scan(&analyzedTable, &analyzedOperation, &analyzedType, &analyzedMessage); err != nil || analyzedType != "status" || analyzedMessage != "OK" {
		t.Fatal("refresh fixture optimizer statistics", err, analyzedType, analyzedMessage)
	}
	for _, branch := range []struct {
		predicate string
		args      []any
	}{
		{"created_at=? AND id<?", []any{mysqlstore.UUID(a), mysqlstore.DateTime{Time: at}, mysqlstore.UUID(ids[500])}},
		{"created_at<?", []any{mysqlstore.UUID(a), mysqlstore.DateTime{Time: at}}},
	} {
		var plan string
		if err := db.QueryRowContext(ctx, "EXPLAIN FORMAT=TREE SELECT id,tenant_id,created_at FROM data_model_index_deliveries WHERE tenant_id=? AND "+branch.predicate+" ORDER BY created_at DESC,id DESC LIMIT 128", branch.args...).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		t.Log(plan)
		if !strings.Contains(plan, "data_model_delivery_tenant") || !strings.Contains(plan, "range scan") {
			t.Fatal("covering keyset range not used", plan)
		}
	}
	// A dedicated account needs SELECT on only these three objects. It cannot
	// read histories or mutate deliveries, and never receives a lease token.
	user := "inspect_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "`" + user + "`@`%`"
	if _, err := db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-inspection-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	policy := installation.AccountPolicy{User: user, Host: "%"}
	for _, name := range []string{"data_model_index_deliveries", "data_model_index_jobs", "data_model_index_delivery_reservations"} {
		policy.Objects = append(policy.Objects, installation.ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT"}})
	}
	grants, err := installation.GrantStatements(database, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := db.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: database, User: user, Password: "isolated-inspection-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	readOnly, err := store.NewDeliveryInspector(reader, database, a)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := readOnly.Page(ctx, nil, 100); err != nil || len(page.Items) != 100 {
		t.Fatalf("read-only inspection %d %v", len(page.Items), err)
	}
	_, err = reader.ExecContext(ctx, "UPDATE data_model_index_deliveries SET state=state WHERE id=?", mysqlstore.UUID(ids[0]))
	mysqlError(t, err, 1142)
	_, err = reader.ExecContext(ctx, "SELECT id FROM data_model_schema_change_log LIMIT 1")
	mysqlError(t, err, 1142)
}
