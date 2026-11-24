package postgres_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAcceptanceRestrictedLoginsAndLargePrivatePublication(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	db := f.db
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, file := range []string{"schema_v2.sql", "schema_v3.sql", "schema_v4.sql"} {
		raw, e := os.ReadFile("../../../../auth-service/internal/platform/" + file)
		if e != nil {
			t.Fatal(e)
		}
		execSQL(string(raw))
	}
	raw, e := os.ReadFile("../../../scripts/source-role-grants.sql")
	if e != nil {
		t.Fatal(e)
	}
	execSQL(string(raw))
	observations := []map[string]any{}
	var writer *pgxpool.Pool
	for _, capability := range []string{"screening_source_reader", "screening_private_writer", "screening_source_collector", ""} {
		role := "acceptance_login_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		password := uuid.NewString() + uuid.NewString()
		// Only generated UUID characters enter this test-only password literal.
		execSQL("CREATE ROLE " + pgx.Identifier{role}.Sanitize() + " LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '" + password + "'")
		t.Cleanup(func() {
			if _, e := db.Exec(context.Background(), "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
				t.Error(e)
			}
		})
		if capability != "" {
			execSQL("GRANT " + pgx.Identifier{capability}.Sanitize() + " TO " + pgx.Identifier{role}.Sanitize())
		}
		cfg := db.Config()
		cfg.ConnConfig.User = role
		cfg.ConnConfig.Password = password
		cfg.MaxConns = 2
		login, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer login.Close()
		if e = login.Ping(ctx); e != nil {
			t.Fatal("actual restricted login failed", e)
		}
		badCfg := cfg.Copy()
		badCfg.ConnConfig.Password = "incorrect-disposable-test-password"
		bad, e := pgxpool.NewWithConfig(ctx, badCfg)
		if e != nil {
			t.Fatal(e)
		}
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		badErr := bad.Ping(probe)
		cancel()
		bad.Close()
		if badErr == nil {
			t.Fatal("database authentication did not reject a wrong password")
		}
		var super, createRole, createDB bool
		if e = login.QueryRow(ctx, `SELECT rolsuper,rolcreaterole,rolcreatedb FROM pg_roles WHERE rolname=current_user`).Scan(&super, &createRole, &createDB); e != nil || super || createRole || createDB {
			t.Fatal("restricted login privilege", e)
		}
		if _, e = login.Exec(ctx, `UPDATE identity.memberships SET roles=ARRAY['platform_admin']`); e == nil {
			t.Fatal("restricted login changed identity authorization")
		}
		if capability == "screening_source_reader" {
			if _, e = login.Exec(ctx, `UPDATE screening.source_datasets SET enabled=false`); e == nil {
				t.Fatal("source reader mutated data")
			}
		}
		if capability == "screening_private_writer" {
			writer = login
			if e = privatelists.CheckDatabaseBinding(ctx, db, writer); e != nil {
				t.Fatal("same primary restricted writer", e)
			}
			if _, e = writer.Exec(ctx, `UPDATE screening.source_connections SET enabled=true`); e == nil {
				t.Fatal("private writer enabled collection")
			}
		}
		if capability == "" {
			if _, e = login.Exec(ctx, `SELECT screening.refresh_source_statistics_v1()`); e == nil {
				t.Fatal("public login invoked owner statistics maintenance")
			}
			rows, readErr := login.Query(ctx, `SELECT * FROM screening.source_records`)
			if readErr == nil {
				rows.Close()
				t.Fatal("unprivileged login read source records")
			}
		}
		observations = append(observations, map[string]any{"capability": capability, "actual_login_verified": true, "wrong_password_rejected": true, "identity_mutation_denied": true, "superuser": false, "create_role": false, "create_database": false})
	}
	if writer == nil {
		t.Fatal("writer login missing")
	}
	execSQL(`UPDATE identity.memberships SET roles=ARRAY['tenant_admin','case_investigator'] WHERE tenant_id=ANY($1::uuid[])`, []uuid.UUID{f.a.Tenant(), f.b.Tenant()})
	author := f.a.Principal()
	reviewer := identity.Principal{Subject: uuid.NewString(), TenantID: author.TenantID, SessionID: uuid.New(), AuthzVersion: 1}
	execSQL(`INSERT INTO identity.users(id,display_name) VALUES($1,'Restricted publication reviewer')`, reviewer.Subject)
	execSQL(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['tenant_admin'])`, reviewer.Subject, reviewer.TenantID)
	execSQL(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, reviewer.SessionID, reviewer.Subject, reviewer.TenantID, uuid.NewString())
	svc := privatelists.Service{DB: writer, Policy: privatelists.TenantAdministratorPolicy()}
	list, e := svc.Create(ctx, author, privatelists.CreateInput{OperationID: uuid.NewString(), Name: "Restricted large fixture", Purpose: "Disposable security acceptance", RightsReference: "Authored fictional records"})
	if e != nil {
		t.Fatal(e)
	}
	var csv strings.Builder
	csv.WriteString("record_id,name,entity_type,source_reference\n")
	for i := 0; i < 5001; i++ {
		fmt.Fprintf(&csv, "restricted-%05d,Fictional Restricted Person %05d,Person,own fixture\n", i, i)
	}
	item, e := svc.Submit(ctx, author, list, privatelists.ImportInput{OperationID: uuid.NewString(), CSV: csv.String()})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Approve(ctx, author, list, item.ID); e == nil {
		t.Fatal("self approval accepted")
	}
	if _, e = svc.Approve(ctx, reviewer, list, item.ID); e != nil {
		t.Fatal("restricted large publication/statistics", e)
	}
	if _, e = svc.GetImport(ctx, f.b.Principal(), list, item.ID); e == nil {
		t.Fatal("foreign administrator read private receipt")
	}
	var estimate float64
	if e = db.QueryRow(ctx, `SELECT reltuples FROM pg_class WHERE oid='screening.source_names'::regclass`).Scan(&estimate); e != nil || estimate < 5000 {
		t.Fatal("large publication did not refresh planner statistics", estimate, e)
	}
	acceptanceReport(t, "restricted-logins", map[string]any{"logins": observations, "private_records": 5001, "different_approver_required": true, "foreign_tenant_admin_denied": true, "same_primary_binding_checked": true, "large_publication_statistics_verified": true, "production_credentials_changed": false, "limitations": []string{"Disposable generated credentials only; no production secret rotation, TLS/network or managed-role acceptance", "These are trusted service capabilities, not per-tenant DB roles or row-level security; tenant authority is enforced by current-identity application contracts"}})
}
