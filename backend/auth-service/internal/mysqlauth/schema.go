package mysqlauth

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

const Owner = "identity_auth"

// Statements adds auth-owned application objects without rewriting the existing
// identity contract or its retained decision-service migration lineage.
func Statements(database string) ([]migrations.Step, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	tables := []struct{ name, body string }{
		// A guard is a lock namespace, not an identity relationship. Its INSERT
		// must not lock a tenant parent before user/membership/session locks.
		{"identity_auth_admin_guards", `tenant_id BINARY(16) PRIMARY KEY`},
		{"identity_auth_external_guards", `identity_key BINARY(32) PRIMARY KEY`},
		{"identity_external_identities", `identity_key BINARY(32) PRIMARY KEY,issuer VARBINARY(2048) NOT NULL,subject VARBINARY(4096) NOT NULL,user_id BINARY(16) NOT NULL,KEY identity_external_user(user_id),FOREIGN KEY(user_id) REFERENCES ` + s + `.identity_users(id)`},
		{"identity_audit_events", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,actor_id VARCHAR(64) NOT NULL,action VARCHAR(64) NOT NULL,target_id BINARY(16) NOT NULL,details JSON NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),KEY identity_audit_tenant(tenant_id,id),FOREIGN KEY(tenant_id) REFERENCES ` + s + `.identity_tenants(id)`},
		{"identity_invitations", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,email VARBINARY(254) NOT NULL,issuer VARBINARY(2048) NOT NULL,roles SET('tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin') NOT NULL CHECK(roles<>''),inviter_id BINARY(16) NOT NULL,inviter_version BIGINT NOT NULL CHECK(inviter_version>0),digest BINARY(32) NOT NULL UNIQUE,version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','cancelled')),expires_at DATETIME(6) NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),accepted_by BINARY(16) NULL,accepted_at DATETIME(6) NULL,pending_email VARBINARY(254) GENERATED ALWAYS AS (CASE WHEN status='pending' THEN email ELSE NULL END) STORED,UNIQUE KEY identity_pending_email(tenant_id,pending_email),KEY identity_invitation_list(tenant_id,id),KEY identity_invitation_created(tenant_id,created_at),FOREIGN KEY(tenant_id,inviter_id) REFERENCES ` + s + `.identity_memberships(tenant_id,user_id),FOREIGN KEY(accepted_by) REFERENCES ` + s + `.identity_users(id),CHECK((status='accepted')=(accepted_by IS NOT NULL AND accepted_at IS NOT NULL))`},
		{"identity_login_transactions", `digest BINARY(32) PRIMARY KEY,binding_digest BINARY(32) NOT NULL,issuer VARBINARY(2048) NOT NULL,nonce VARCHAR(64) NOT NULL,verifier VARCHAR(64) NOT NULL,invitation_digest BINARY(32) NULL,expires_at DATETIME(6) NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),KEY identity_login_expiry(expires_at,digest)`},
		{"identity_invitation_delivery", `invitation_id BINARY(16) PRIMARY KEY,generation BIGINT NOT NULL CHECK(generation>0),payload BLOB NULL,state VARCHAR(16) NOT NULL CHECK(state IN ('queued','sending','sent','failed','cancelled')),attempts INT NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),next_attempt DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),requested_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),lease_id BINARY(16) NULL,lease_until DATETIME(6) NULL,sent_at DATETIME(6) NULL,KEY identity_delivery_due(state,next_attempt,invitation_id),KEY identity_delivery_recovery(state,lease_until,invitation_id),FOREIGN KEY(invitation_id) REFERENCES ` + s + `.identity_invitations(id),CHECK((state='sending')=(lease_id IS NOT NULL AND lease_until IS NOT NULL)),CHECK(state NOT IN ('queued','sending') OR payload IS NOT NULL)`},
	}
	out := []migrations.Step{}
	for n, t := range tables {
		out = append(out, migrations.Step{ID: fmt.Sprintf("identity_auth_v001_%02d", n+1), Kind: "TABLE", Name: t.name, SQL: "CREATE TABLE " + s + "." + t.name + "(" + t.body + ") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin"})
	}
	return out, nil
}

func RuntimeGrants(database, user string, worker bool) ([]string, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(user)
	if e != nil {
		return nil, e
	}
	account := u + "@'%'"
	grants := []string{}
	read := []string{"identity_invitations", "identity_invitation_delivery", "identity_auth_schema_migrations", "identity_schema_migrations"}
	if !worker {
		read = append(read, "identity_tenants", "identity_users", "identity_memberships", "identity_sessions")
	}
	for _, table := range read {
		grants = append(grants, "GRANT SELECT ON "+s+"."+table+" TO "+account)
	}
	if worker {
		return append(grants, "GRANT UPDATE ON "+s+".identity_invitation_delivery TO "+account, "GRANT SELECT,DELETE ON "+s+".identity_login_transactions TO "+account), nil
	}
	for _, table := range []string{"identity_users", "identity_memberships", "identity_sessions", "identity_external_identities", "identity_invitations", "identity_invitation_delivery", "identity_login_transactions", "identity_auth_admin_guards", "identity_auth_external_guards"} {
		grants = append(grants, "GRANT SELECT,INSERT,UPDATE ON "+s+"."+table+" TO "+account)
	}
	grants = append(grants, "GRANT SELECT,INSERT ON "+s+".identity_audit_events TO "+account, "GRANT DELETE ON "+s+".identity_login_transactions TO "+account)
	return grants, nil
}
