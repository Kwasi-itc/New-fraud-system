// Package mysqlidentity implements staged auth-owned decision contracts. It
// neither selects MySQL in application startup nor grants roles during install.
package mysqlidentity

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

// SET bit ordering is a versioned contract used only by auth-owned functions.
// The tenant registry here owns identity activation, not model/table metadata.
func Statements(database, definer string) ([]migrations.Step, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(definer)
	if err != nil {
		return nil, err
	}
	var out []migrations.Step
	add := func(kind, name, sql string) {
		out = append(out, migrations.Step{ID: fmt.Sprintf("identity_v001_%04d", len(out)+1), Kind: kind, Name: name, SQL: sql})
	}
	tables := []struct{ name, body string }{
		{"identity_tenants", `id BINARY(16) PRIMARY KEY,active BOOLEAN NOT NULL CHECK(active IN(0,1))`},
		{"identity_users", `id BINARY(16) PRIMARY KEY,display_name VARCHAR(200) NOT NULL CHECK(CHAR_LENGTH(display_name)>0),active BOOLEAN NOT NULL CHECK(active IN(0,1))`},
		{"identity_memberships", fmt.Sprintf(`tenant_id BINARY(16) NOT NULL,user_id BINARY(16) NOT NULL,
 roles SET('tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin') NOT NULL CHECK(roles<>''),
 active BOOLEAN NOT NULL CHECK(active IN(0,1)),version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 PRIMARY KEY(tenant_id,user_id),KEY identity_membership_user(user_id,tenant_id),
 FOREIGN KEY(tenant_id) REFERENCES %s.identity_tenants(id),FOREIGN KEY(user_id) REFERENCES %s.identity_users(id)`, s, s)},
		{"identity_sessions", fmt.Sprintf(`id BINARY(16) PRIMARY KEY,digest BINARY(32) NOT NULL UNIQUE,
 tenant_id BINARY(16) NOT NULL,user_id BINARY(16) NOT NULL,read_only BOOLEAN NOT NULL DEFAULT FALSE CHECK(read_only IN(0,1)),
 created_at DATETIME(6) NOT NULL,expires_at DATETIME(6) NOT NULL,idle_until DATETIME(6) NOT NULL,revoked_at DATETIME(6) NULL,
 CHECK(expires_at>created_at AND idle_until>created_at AND idle_until<=expires_at),
 KEY identity_sessions_user(tenant_id,user_id,id),KEY identity_sessions_expiry(expires_at,id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES %s.identity_memberships(tenant_id,user_id)`, s)},
	}
	for _, t := range tables {
		add("TABLE", t.name, "CREATE TABLE "+s+"."+t.name+"("+t.body+") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin")
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='identity integrity checks required'; END IF; "
			if event == "UPDATE" {
				cols := []string{"id"}
				if t.name == "identity_memberships" {
					cols = []string{"tenant_id", "user_id"}
					body += "IF NEW.version<>OLD.version+1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='membership next version required'; END IF; "
				}
				if t.name == "identity_sessions" {
					cols = append(cols, "digest", "tenant_id", "user_id", "created_at")
					body += "IF OLD.revoked_at IS NOT NULL AND NOT(NEW.revoked_at<=>OLD.revoked_at) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='session revocation is final'; END IF; "
				}
				for _, col := range cols {
					body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable identity'; END IF; ", col, col)
				}
			}
			name := t.name + "_" + strings.ToLower(event)
			add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", u, s, name, event, s, t.name, body))
		}
	}
	// Locking reads fetch current row values. Expiry is evaluated after all waits
	// with SYSDATE, since NOW/CURRENT_TIMESTAMP are fixed at statement start.
	add("FUNCTION", "identity_session_roles_v1", fmt.Sprintf(`CREATE DEFINER=%s@%s FUNCTION %s.identity_session_roles_v1(
 actor BINARY(16),tenant BINARY(16),sid BINARY(16),expected_version BIGINT,take_locks BOOLEAN)
 RETURNS BIGINT NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
 BEGIN
 DECLARE ua,ma,ta,ro BOOLEAN DEFAULT FALSE; DECLARE roles_mask BIGINT DEFAULT 0; DECLARE mv BIGINT DEFAULT 0;
 DECLARE expiry,idle,revoked DATETIME(6); DECLARE missing BOOLEAN DEFAULT FALSE;
 DECLARE CONTINUE HANDLER FOR NOT FOUND SET missing=TRUE;
 IF actor IS NULL OR tenant IS NULL OR sid IS NULL OR expected_version IS NULL OR expected_version<1 OR take_locks IS NULL THEN RETURN 0; END IF;
 IF take_locks THEN
 SELECT active INTO ua FROM %s.identity_users WHERE id=actor FOR SHARE;
 SELECT active,roles+0,version INTO ma,roles_mask,mv FROM %s.identity_memberships WHERE tenant_id=tenant AND user_id=actor FOR SHARE;
 SELECT expires_at,idle_until,revoked_at,read_only INTO expiry,idle,revoked,ro FROM %s.identity_sessions WHERE id=sid AND tenant_id=tenant AND user_id=actor FOR SHARE;
 SELECT active INTO ta FROM %s.identity_tenants WHERE id=tenant FOR SHARE;
 ELSE
 SELECT active INTO ua FROM %s.identity_users WHERE id=actor;
 SELECT active,roles+0,version INTO ma,roles_mask,mv FROM %s.identity_memberships WHERE tenant_id=tenant AND user_id=actor;
 SELECT expires_at,idle_until,revoked_at,read_only INTO expiry,idle,revoked,ro FROM %s.identity_sessions WHERE id=sid AND tenant_id=tenant AND user_id=actor;
 SELECT active INTO ta FROM %s.identity_tenants WHERE id=tenant;
 END IF;
 IF missing OR NOT ua OR NOT ma OR NOT ta OR mv<>expected_version OR revoked IS NOT NULL OR expiry<=SYSDATE(6) OR idle<=SYSDATE(6) OR (take_locks AND ro) THEN RETURN 0; END IF;
 RETURN roles_mask;
 END`, u, "`localhost`", s, s, s, s, s, s, s, s, s))
	add("FUNCTION", "identity_authorize_decision_v1", fmt.Sprintf(`CREATE DEFINER=%s@%s FUNCTION %s.identity_authorize_decision_v1(
 actor BINARY(16),tenant BINARY(16),sid BINARY(16),expected_version BIGINT,action VARCHAR(64),take_locks BOOLEAN)
 RETURNS BOOLEAN NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
 BEGIN DECLARE roles_mask BIGINT DEFAULT 0; DECLARE writes BOOLEAN;
 IF action IS NULL OR action NOT IN('decision.read','decision.author','decision.publish','decision.workflow.read','decision.workflow.author','decision.list.read','decision.list.mutate') THEN RETURN FALSE; END IF;
 SET writes=action IN('decision.author','decision.publish','decision.workflow.author','decision.list.mutate');
 IF take_locks IS NULL OR take_locks<>writes THEN RETURN FALSE; END IF;
 SET roles_mask=%s.identity_session_roles_v1(actor,tenant,sid,expected_version,take_locks);
 RETURN CASE
 WHEN action IN('decision.workflow.read','decision.workflow.author') THEN (roles_mask & 64)<>0 AND (roles_mask & 6)<>0
 WHEN action IN('decision.read','decision.list.read') THEN (roles_mask & 120)<>0
 WHEN action IN('decision.author','decision.list.mutate') THEN (roles_mask & 80)<>0
 WHEN action='decision.publish' THEN (roles_mask & 96)<>0 ELSE FALSE END;
 END`, u, "`localhost`", s, s))
	return out, nil
}

func Plan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := Statements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(steps) != len(reference) {
		return migrations.Plan{}, fmt.Errorf("identity reference length mismatch")
	}
	for i, s := range steps {
		r := reference[i]
		if s.ID != r.ID || s.Kind != r.Kind || s.Name != r.Name || s.SQL != r.SQL || r.Before != "" {
			return migrations.Plan{}, fmt.Errorf("identity reference mismatch at %d", i)
		}
		steps[i].After = r.After
	}
	p := migrations.Plan{Database: database, Owner: "identity", Steps: steps}
	return p, p.Validate()
}

func DefinerGrants(database, definer string) ([]string, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(definer)
	if e != nil {
		return nil, e
	}
	var out []string
	for _, table := range []string{"identity_tenants", "identity_users", "identity_memberships", "identity_sessions"} {
		out = append(out, "GRANT SELECT,TRIGGER ON "+s+"."+table+" TO "+u+"@`localhost`")
	}
	out = append(out, "GRANT EXECUTE ON FUNCTION "+s+".identity_session_roles_v1 TO "+u+"@`localhost`")
	out = append(out, "GRANT EXECUTE ON FUNCTION "+s+".identity_authorize_decision_v1 TO "+u+"@`localhost`")
	return out, nil
}
