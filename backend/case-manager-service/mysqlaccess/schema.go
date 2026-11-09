// Package mysqlaccess owns the staged case authorization tables and contracts.
// Install identity's owner migration first in the same logical database.
package mysqlaccess

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

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
		out = append(out, migrations.Step{ID: fmt.Sprintf("case_access_v001_%04d", len(out)+1), Kind: kind, Name: name, SQL: sql})
	}
	for _, table := range []string{"case_manager_inboxes", "case_manager_tags"} {
		add("TABLE", table, fmt.Sprintf(`CREATE TABLE %s.%s(id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,
 name VARCHAR(200) NOT NULL,active BOOLEAN NOT NULL CHECK(active IN(0,1)),
 UNIQUE KEY case_access_tenant(tenant_id,id),FOREIGN KEY(tenant_id) REFERENCES %s.identity_tenants(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, s, table, s))
	}
	add("TABLE", "case_manager_inbox_users", fmt.Sprintf(`CREATE TABLE %s.case_manager_inbox_users(
 tenant_id BINARY(16) NOT NULL,inbox_id BINARY(16) NOT NULL,user_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,inbox_id,user_id),KEY case_access_user(tenant_id,user_id,inbox_id),
 FOREIGN KEY(tenant_id,inbox_id) REFERENCES %s.case_manager_inboxes(tenant_id,id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES %s.identity_memberships(tenant_id,user_id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, s, s, s))
	for _, table := range []string{"case_manager_inboxes", "case_manager_tags", "case_manager_inbox_users"} {
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='case integrity checks required'; END IF; "
			if event == "UPDATE" {
				cols := []string{"id", "tenant_id"}
				if table == "case_manager_inbox_users" {
					cols = []string{"tenant_id", "inbox_id", "user_id"}
				}
				for _, col := range cols {
					body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable case identity'; END IF; ", col, col)
				}
			}
			name := table + "_" + strings.ToLower(event)
			add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", u, s, name, event, s, table, body))
		}
	}
	for _, kind := range []string{"inboxes", "tags"} {
		limit := 500
		if kind == "tags" {
			limit = 25
		}
		query := fmt.Sprintf("SELECT i.id,i.active,TRUE FROM %s.case_manager_tags i WHERE i.tenant_id=tenant AND i.id IN (SELECT UUID_TO_BIN(j.id) FROM JSON_TABLE(ids,'$[*]' COLUMNS(id VARCHAR(36) PATH '$')) j) ORDER BY i.id", s)
		if kind == "inboxes" {
			query = fmt.Sprintf("SELECT i.id,i.active,(m.user_id IS NOT NULL) FROM %s.case_manager_inboxes i LEFT JOIN %s.case_manager_inbox_users m ON m.tenant_id=i.tenant_id AND m.inbox_id=i.id AND m.user_id=actor WHERE i.tenant_id=tenant AND i.id IN (SELECT UUID_TO_BIN(j.id) FROM JSON_TABLE(ids,'$[*]' COLUMNS(id VARCHAR(36) PATH '$')) j) ORDER BY i.id", s, s)
		}
		name := "case_manager_decision_" + kind + "_v1"
		add("FUNCTION", name, fmt.Sprintf(`CREATE DEFINER=%s@%s FUNCTION %s.%s(actor BINARY(16),tenant BINARY(16),sid BINARY(16),expected_version BIGINT,ids JSON,require_active BOOLEAN,take_locks BOOLEAN)
 RETURNS JSON NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
 BEGIN
 DECLARE roles_mask BIGINT DEFAULT 0; DECLARE done BOOLEAN DEFAULT FALSE; DECLARE item BINARY(16); DECLARE active_item,member BOOLEAN; DECLARE result JSON DEFAULT(JSON_ARRAY());
 DECLARE unlocked CURSOR FOR %s;
 DECLARE locked CURSOR FOR %s FOR SHARE;
 DECLARE CONTINUE HANDLER FOR NOT FOUND SET done=TRUE;
 IF NOT COALESCE(JSON_TYPE(ids)='ARRAY' AND JSON_LENGTH(ids)<=%d,FALSE) OR require_active IS NULL OR take_locks IS NULL THEN RETURN NULL; END IF;
 IF EXISTS(SELECT 1 FROM JSON_TABLE(ids,'$[*]' COLUMNS(id VARCHAR(100) PATH '$')) j WHERE NOT IS_UUID(j.id) OR j.id='00000000-0000-0000-0000-000000000000') THEN RETURN NULL; END IF;
 IF (SELECT COUNT(DISTINCT LOWER(j.id)) FROM JSON_TABLE(ids,'$[*]' COLUMNS(id VARCHAR(100) PATH '$')) j)<>JSON_LENGTH(ids) THEN RETURN NULL; END IF;
 SET roles_mask=%s.identity_session_roles_v1(actor,tenant,sid,expected_version,take_locks);
 IF (roles_mask & 64)=0 OR (roles_mask & 6)=0 THEN RETURN NULL; END IF;
 IF take_locks THEN OPEN locked; ELSE OPEN unlocked; END IF;
 items: LOOP
 IF take_locks THEN FETCH locked INTO item,active_item,member; ELSE FETCH unlocked INTO item,active_item,member; END IF;
 IF done THEN LEAVE items; END IF;
 IF (NOT require_active OR active_item) AND ((roles_mask & 2)<>0 OR member) THEN SET result=JSON_ARRAY_APPEND(result,'$',BIN_TO_UUID(item)); END IF;
 END LOOP;
 IF take_locks THEN CLOSE locked; ELSE CLOSE unlocked; END IF;
 IF %s.identity_session_roles_v1(actor,tenant,sid,expected_version,take_locks)=0 THEN RETURN NULL; END IF;
 RETURN result; END`, u, "`localhost`", s, name, query, query, limit, s, s))
		batch := "case_manager_authorize_decision_" + kind + "_v1"
		add("FUNCTION", batch, fmt.Sprintf(`CREATE DEFINER=%s@%s FUNCTION %s.%s(actor BINARY(16),tenant BINARY(16),sid BINARY(16),expected_version BIGINT,ids JSON,require_active BOOLEAN,take_locks BOOLEAN)
 RETURNS BOOLEAN NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
 RETURN COALESCE(JSON_LENGTH(%s.%s(actor,tenant,sid,expected_version,ids,require_active,take_locks))=JSON_LENGTH(ids),FALSE)`, u, "`localhost`", s, batch, s, name))
	}
	add("FUNCTION", "case_manager_authorize_decision_inbox_v1", fmt.Sprintf(`CREATE DEFINER=%s@%s FUNCTION %s.case_manager_authorize_decision_inbox_v1(actor BINARY(16),tenant BINARY(16),sid BINARY(16),expected_version BIGINT,inbox BINARY(16),require_active BOOLEAN,take_locks BOOLEAN)
 RETURNS BOOLEAN NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
 RETURN %s.case_manager_authorize_decision_inboxes_v1(actor,tenant,sid,expected_version,JSON_ARRAY(BIN_TO_UUID(inbox)),require_active,take_locks)`, u, "`localhost`", s, s))
	add("FUNCTION", "case_manager_visible_decision_inboxes_v1", fmt.Sprintf(`CREATE DEFINER=%s@%s FUNCTION %s.case_manager_visible_decision_inboxes_v1(actor BINARY(16),tenant BINARY(16),sid BINARY(16),expected_version BIGINT,ids JSON)
 RETURNS JSON NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
 RETURN %s.case_manager_decision_inboxes_v1(actor,tenant,sid,expected_version,ids,FALSE,FALSE)`, u, "`localhost`", s, s))
	return out, nil
}
func Plan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := Statements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(steps) != len(reference) {
		return migrations.Plan{}, fmt.Errorf("case access reference length mismatch")
	}
	for i, s := range steps {
		r := reference[i]
		if s.ID != r.ID || s.Kind != r.Kind || s.Name != r.Name || s.SQL != r.SQL || r.Before != "" {
			return migrations.Plan{}, fmt.Errorf("case access reference mismatch at %d", i)
		}
		steps[i].After = r.After
	}
	p := migrations.Plan{Database: database, Owner: "case_manager", Steps: steps}
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
	for _, table := range []string{"case_manager_inboxes", "case_manager_tags", "case_manager_inbox_users"} {
		out = append(out, "GRANT SELECT,TRIGGER ON "+s+"."+table+" TO "+u+"@`localhost`")
	}
	for _, name := range []string{"identity_session_roles_v1", "case_manager_decision_inboxes_v1", "case_manager_decision_tags_v1", "case_manager_authorize_decision_inboxes_v1", "case_manager_authorize_decision_inbox_v1", "case_manager_authorize_decision_tags_v1"} {
		out = append(out, "GRANT EXECUTE ON FUNCTION "+s+"."+name+" TO "+u+"@`localhost`")
	}
	return out, nil
}
