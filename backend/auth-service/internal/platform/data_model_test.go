package platform

import (
	"context"
	"testing"
)

func TestDataModelRoleMigrationAndRevocation(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, adminRaw := provision(t, s, tenant, "admin", "tenant_admin")
	// Reconstruct the v1 role constraint to exercise an actual existing-schema upgrade.
	if _, err := s.DB.Exec(ctx, `DELETE FROM identity.schema_migrations WHERE version=2;
 ALTER TABLE identity.memberships DROP CONSTRAINT memberships_roles_check;
 ALTER TABLE identity.memberships ADD CONSTRAINT memberships_roles_check CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator']::text[])`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("replay", err)
	}
	admin, err := s.Resolve(ctx, adminRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.Roles) != 1 || admin.Roles[0] != "tenant_admin" {
		t.Fatal("migration granted new authority")
	}
	for _, role := range []string{"platform_viewer", "platform_builder", "platform_publisher", "platform_admin"} {
		user, raw := provision(t, s, tenant, role, role)
		v, err := s.Resolve(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, write := range []bool{false, true} {
			var allowed bool
			err := s.DB.QueryRow(ctx, "SELECT identity.authorize_data_model_v1($1,$2,$3,$4,$5,false)", user.String(), tenant, v.ID, v.Version, write).Scan(&allowed)
			if err != nil || allowed != (role == "platform_admin" || !write) {
				t.Fatalf("role=%s write=%v allowed=%v err=%v", role, write, allowed, err)
			}
		}
		if allowed(t, s, v, false) {
			t.Fatalf("platform role %s granted case access", role)
		}
		if err := s.ChangeMember(ctx, adminRaw, user, v.Version, []string{"platform_viewer"}, false); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Resolve(ctx, raw); err == nil {
			t.Fatal("membership change did not revoke session")
		}
	}
}
