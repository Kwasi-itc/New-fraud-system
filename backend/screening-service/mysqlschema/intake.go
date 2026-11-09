package mysqlschema

import (
	"fmt"
)

// Append to the existing owner lineage. Intake keys serialize only one logical
// screening; they are retained independently of transport cleanup. No FK from
// the reservation to the decision execution: that would acquire an execution
// lock before the screening source lock used by publication.
func intakeStatements(s, u string, add func(string, string, string)) {
	add("TABLE", "screening_intake_keys", fmt.Sprintf(`CREATE TABLE %s.screening_intake_keys(
 tenant_id BINARY(16) NOT NULL,execution_id BINARY(16) NOT NULL,payload_sha256 BINARY(32) NOT NULL,
 PRIMARY KEY(tenant_id,execution_id)
 ) ENGINE=InnoDB`, s))
	add("TABLE", "screening_intake_receipts", fmt.Sprintf(`CREATE TABLE %s.screening_intake_receipts(
 tenant_id BINARY(16) NOT NULL,execution_id BINARY(16) NOT NULL,
 domain_attempt INT NOT NULL CHECK(domain_attempt>0),redrive_generation INT NOT NULL CHECK(redrive_generation>=0),
 provider_key VARBINARY(76) NOT NULL CHECK(OCTET_LENGTH(provider_key)=76),payload_sha256 BINARY(32) NOT NULL,
 screening_id BINARY(16) NOT NULL,accepted_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,execution_id,domain_attempt,redrive_generation),UNIQUE KEY screening_receipt_key(tenant_id,provider_key),
 FOREIGN KEY(tenant_id,execution_id) REFERENCES %s.screening_intake_keys(tenant_id,execution_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES %s.screening_screenings(tenant_id,id)
 ) ENGINE=InnoDB`, s, s, s))
	for _, table := range []string{"screening_intake_keys", "screening_intake_receipts"} {
		columns := []string{"tenant_id", "execution_id", "payload_sha256"}
		if table == "screening_intake_receipts" {
			columns = append(columns, "domain_attempt", "redrive_generation", "provider_key", "screening_id", "accepted_at")
		}
		body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening intake integrity required'; END IF; "
		for _, col := range columns {
			body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable screening intake'; END IF; ", col, col)
		}
		add("TRIGGER", table+"_update", fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s_update BEFORE UPDATE ON %s.%s FOR EACH ROW BEGIN %s END", u, s, table, s, table, body))
		add("TRIGGER", table+"_delete", fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s_delete BEFORE DELETE ON %s.%s FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening intake retention required'", u, s, table, s, table))
	}
	add("TRIGGER", "screening_intake_receipts_insert", fmt.Sprintf(`CREATE DEFINER=%s@%s TRIGGER %s.screening_intake_receipts_insert BEFORE INSERT ON %s.screening_intake_receipts FOR EACH ROW BEGIN
 DECLARE bound INT DEFAULT 0;
 IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening intake integrity required'; END IF;
 SELECT COUNT(*) INTO bound FROM %s.screening_screenings s JOIN %s.screening_intake_keys k ON k.tenant_id=s.tenant_id AND k.execution_id=s.execution_id
 WHERE s.tenant_id=NEW.tenant_id AND s.id=NEW.screening_id AND s.execution_id=NEW.execution_id AND k.payload_sha256=NEW.payload_sha256 FOR SHARE;
 IF bound<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening intake receipt binding mismatch'; END IF;
 END`, u, "`localhost`", s, s, s, s))
	add("TABLE", "screening_screenings", fmt.Sprintf("ALTER TABLE %s.screening_screenings ADD KEY screening_pending(tenant_id,status,created_at,id)", s))
}
