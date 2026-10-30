package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlrelease"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func releaseRuntimeAccount(t *testing.T, f platformFixture, role string) *sql.DB {
	t.Helper()
	user := "de_release_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	var host string
	if err := f.db.QueryRow("SELECT SUBSTRING_INDEX(USER(),'@',-1)").Scan(&host); err != nil {
		t.Fatal(err)
	}
	grants, err := mysqlrelease.OwnerGrants(f.database, user, host, role)
	if err != nil {
		t.Fatal(err)
	}
	account := "`" + user + "`@'" + host + "'"
	if _, err := f.db.Exec("CREATE USER " + account + " IDENTIFIED BY 'fixture-release-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	for _, grant := range grants {
		if _, err := f.db.Exec(grant); err != nil {
			t.Fatal(grant, err)
		}
	}
	db, err := mysqlstore.Open(context.Background(), platformConfig(f.database, user, "fixture-release-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMySQLReleaseRuntimeGrantBoundaryAndWorkerDispatch(t *testing.T) {
	f, _, _ := dispatchFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	// A sentinel stands for an unrelated owner, not a provider implementation.
	if _, err := f.db.Exec("CREATE TABLE " + schema + ".fixture_foreign_owner(id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "worker"} {
		t.Run(role, func(t *testing.T) {
			db := releaseRuntimeAccount(t, f.platformFixture, role)
			for _, query := range []string{
				"SELECT * FROM " + schema + ".decision_engine_exact_keys LIMIT 0",
				"SELECT * FROM " + schema + ".decision_engine_exact_key_buckets LIMIT 0",
				"SELECT * FROM " + schema + ".fixture_foreign_owner LIMIT 0",
				"INSERT INTO " + schema + ".fixture_foreign_owner VALUES(1)",
				"UPDATE " + schema + ".decision_engine_schema_migrations SET step_no=2 WHERE step_no=1",
				"DROP TABLE " + schema + ".decision_engine_record_tags",
				"ALTER TABLE " + schema + ".decision_engine_record_tags ADD COLUMN forged INT",
				"CALL " + schema + ".decision_engine_resolve_exact_key(_binary'x',UUID_TO_BIN(UUID()),_binary'x',@key)",
			} {
				_, err := db.ExecContext(ctx, query)
				var sqlErr *driver.MySQLError
				if !errors.As(err, &sqlErr) || (sqlErr.Number != 1142 && sqlErr.Number != 1370) {
					t.Fatal("expected explicit privilege denial", query, err)
				}
			}
			// UPDATE(id) authorizes locking but cannot change an existing key.
			// Effect receipt guards compare values; no-op UPDATE is harmless and
			// need not raise, unlike an actual attempted rewrite tested here.
			_, mutationErr := db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_receipts SET id=UUID_TO_BIN(UUID()) LIMIT 1")
			var mutationSQL *driver.MySQLError
			if !errors.As(mutationErr, &mutationSQL) || mutationSQL.Number != 1644 {
				t.Fatal("receipt key rewrite bypassed owner guard", mutationErr)
			}
			if _, err := db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_receipts SET domain_attempt=99 LIMIT 1"); err == nil {
				t.Fatal("key-column privilege expanded to receipt body")
			}
			if role == "api" {
				if _, err := db.ExecContext(ctx, "DELETE FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=UUID_TO_BIN(UUID())"); err == nil {
					t.Fatal("API can clean deliveries")
				}
				if _, err := db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_dispatch_intents(receipt_id) VALUES(UUID_TO_BIN(UUID()))"); err == nil {
					t.Fatal("API can forge send intent")
				}
				return
			}
			owner, err := NewQueuedEffectStore(db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, nil, effectQueuePolicies())
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
				delivery, err := NewEffectDeliveryStore(db, f.database, f.tenant, kind, effectQueuePolicies()[kind].Name, time.Minute, allowEffectWorkerFixture)
				if err != nil {
					t.Fatal(kind, err)
				}
				store, err := NewEffectDispatchStore(owner, delivery)
				if err != nil {
					t.Fatal(kind, err)
				}
				lease, err := store.Claim(ctx)
				if err != nil || lease == nil {
					t.Fatal(kind, lease, err)
				}
				intent, err := store.Prepare(ctx, *lease)
				if err != nil {
					t.Fatal(kind, err)
				}
				if err := store.Complete(ctx, *lease, intent.Request, acceptedDispatch(intent), time.Second); err != nil {
					t.Fatal(kind, err)
				}
			}
			for _, table := range []string{"dispatch_intents", "effect_job_receipts", "execution_job_receipts", "decisions"} {
				if _, err := db.ExecContext(ctx, "DELETE FROM "+schema+".decision_engine_"+table); err == nil {
					t.Fatal("worker can erase permanent records", table)
				}
			}
		})
	}
}
