package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
)

func TestAccountDirectoryIndexesAndTenantBoundaries(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, raw := provision(t, s, tenant, "directory-admin", "tenant_admin")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO identity.users(id,display_name) SELECT md5('directory-member-'||g::text)::uuid,'Directory member '||lpad(g::text,5,'0') FROM generate_series(1,10000) g`)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) SELECT $1,md5('directory-member-'||g::text)::uuid,ARRAY['platform_viewer'] FROM generate_series(1,10000) g`, tenant)
	exec(`INSERT INTO identity.clients(tenant_id,id,name,scopes) SELECT $1,md5('directory-client-'||g::text)::uuid,'Directory connector '||lpad(g::text,5,'0'),ARRAY['decisions:read'] FROM generate_series(1,10000) g`, tenant)
	target, _ := provision(t, s, tenant, "Target_Account", "platform_viewer")
	_, _ = provision(t, s, tenant, "TargetXAccount", "platform_viewer")
	other := uuid.New()
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Other',$2,'active',now(),now())`, other, "tenant_"+strings.ReplaceAll(other.String(), "-", ""))
	_, _ = provision(t, s, other, "Target_Alien", "platform_viewer")
	caller, err := s.Resolve(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	p := principal(caller)
	targetClient, err := s.SaveClient(ctx, p, uuid.Nil, ClientChange{Name: "Target_Connector", Scopes: []string{"decisions:read"}, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveClient(ctx, p, uuid.Nil, ClientChange{Name: "TargetXConnector", Scopes: []string{"decisions:read"}, Active: true}); err != nil {
		t.Fatal(err)
	}
	exec(`ANALYZE identity.users; ANALYZE identity.memberships; ANALYZE identity.member_roles; ANALYZE identity.roles; ANALYZE identity.clients`)
	members, err := s.FilteredMembers(ctx, raw, uuid.Nil, "Target_", "platform_viewer")
	if err != nil || len(members) != 1 || members[0].UserID != target || members[0].RoleName == "" {
		t.Fatal("prefix escaping, role or tenant filter failed", len(members), err)
	}
	clients, err := s.FilteredClients(ctx, p, uuid.Nil, "Target_")
	if err != nil || len(clients) != 1 || clients[0].ID != targetClient.ID {
		t.Fatal("client prefix escaping failed", len(clients), err)
	}
	page, err := s.FilteredMembers(ctx, raw, uuid.Nil, "Directory member", "")
	if err != nil || len(page) != 101 {
		t.Fatal("unbounded or incomplete directory page", len(page), err)
	}
	next, err := s.FilteredMembers(ctx, raw, page[99].UserID, "Directory member", "")
	if err != nil || len(next) != 101 || next[0].UserID != page[100].UserID {
		t.Fatal("directory cursor skipped or duplicated records", err)
	}
	custom, err := s.SaveRole(ctx, p, "", RoleChange{Name: "Directory custom role", Capabilities: []string{"cases:read"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Role(ctx, p, custom.ID); err != nil || got.ID != custom.ID {
		t.Fatal("exact role lookup failed", err)
	}
	foreign := p
	foreign.TenantID = other
	for _, err := range []error{func() error { _, err := s.Role(ctx, foreign, custom.ID); return err }(), func() error { _, err := s.Client(ctx, foreign, targetClient.ID); return err }()} {
		var status *auth.Error
		if !errors.As(err, &status) || status.Code != 404 {
			t.Fatal("foreign detail leaked or wrong error", err)
		}
	}
	explain := func(label, sql, index string, args []any) {
		t.Helper()
		rows, err := s.DB.Query(ctx, "EXPLAIN (ANALYZE,BUFFERS) "+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		text := strings.Join(plan, "\n")
		t.Log(label + "\n" + text)
		if !strings.Contains(text, index) {
			t.Fatalf("representative selective query did not use %s", index)
		}
	}
	memberSQL, memberArgs := memberDirectoryQuery(tenant, uuid.Nil, "Target_", "platform_viewer")
	explain("10,000-member tenant prefix query", memberSQL, "users_display_name_prefix_idx", memberArgs)
	clientSQL, clientArgs := clientDirectoryQuery(tenant, uuid.Nil, "Target_")
	explain("10,000-client tenant prefix query", clientSQL, "clients_name_prefix_idx", clientArgs)
}
