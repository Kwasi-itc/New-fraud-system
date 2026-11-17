package postgres_test

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

func TestPrivateWriterUsesAuthOwnerReadContractWithoutIdentityWritePrivileges(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	if _, e := f.db.Exec(ctx, `UPDATE identity.memberships SET roles=ARRAY['tenant_admin','case_investigator'] WHERE tenant_id=ANY($1::uuid[])`, []uuid.UUID{f.a.Tenant(), f.b.Tenant()}); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"../../../../auth-service/internal/platform/schema_v4.sql", "../../../scripts/source-role-grants.sql"} {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.db.Exec(ctx, string(raw)); e != nil {
			t.Fatal(path, e)
		}
	}
	cfg := f.db.Config()
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, e := c.Exec(ctx, `SET ROLE screening_private_writer`)
		return e
	}
	writer, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Close()
	var canUpdate bool
	if e = writer.QueryRow(ctx, `SELECT has_table_privilege(current_user,'identity.memberships','UPDATE') OR has_table_privilege(current_user,'identity.users','UPDATE') OR has_table_privilege(current_user,'identity.sessions','UPDATE')`).Scan(&canUpdate); e != nil || canUpdate {
		t.Fatal("identity mutation privilege", canUpdate, e)
	}
	policy, _ := privatelists.NewPolicy("tenant_admin", "tenant_admin")
	svc := privatelists.Service{DB: writer, Policy: policy}
	list, e := svc.Create(ctx, f.a.Principal(), privatelists.CreateInput{OperationID: uuid.NewString(), Name: "Restricted writer", Purpose: "Fictional integration", RightsReference: "Own synthetic fixture"})
	if e != nil {
		t.Fatal("restricted creation", e)
	}
	item, e := svc.Submit(ctx, f.a.Principal(), list, privatelists.ImportInput{OperationID: uuid.NewString(), CSV: "record_id,name,entity_type,source_reference\n001,Restricted Fictional Company,Organization,fixture\n"})
	if e != nil || item.Status != "pending_review" {
		t.Fatal("restricted import", item, e)
	}
	if _, e = svc.Detail(ctx, f.a.Principal(), list, nil); e != nil {
		t.Fatal("restricted read", e)
	}
	// The definer contract exposes only the bounded authorization boolean/locks;
	// this role cannot mutate accounts, grant roles, or collect a source.
	if _, e = writer.Exec(ctx, `UPDATE identity.memberships SET active=false WHERE tenant_id=$1`, f.a.Tenant()); e == nil {
		t.Fatal("writer changed auth state")
	}
	if _, e = writer.Exec(ctx, `UPDATE screening.source_connections SET enabled=true`); e == nil {
		t.Fatal("private writer enabled a global collector")
	}
	if _, e = svc.List(ctx, f.b.Principal(), ""); e != nil {
		t.Fatal("own foreign-tenant empty discovery", e)
	}
	var denied bool
	if e = f.db.QueryRow(ctx, `SELECT identity.authorize_roles_v1($1,$2,$3,1,ARRAY['invented_role'],true)`, f.a.Principal().Subject, f.a.Tenant(), f.a.Principal().SessionID).Scan(&denied); e != nil || denied {
		t.Fatal("invented role granted", e)
	}
}
