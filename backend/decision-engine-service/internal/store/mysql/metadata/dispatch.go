package metadata

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// DispatchStatements appends to the unchanged 145-step cancellation lineage.
// Intents and terminal receipts are permanent, decision-owned reconciliation
// evidence. Deleting retained deliveries never permits a second initial send.
func DispatchStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := EffectCancellationStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	add := func(kind, name, sql string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: sql})
	}
	table := "decision_engine_dispatch_intents"
	add("TABLE", table, fmt.Sprintf(`CREATE TABLE %s.%s(
 receipt_id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,
 kind VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 resource_id BINARY(16) NOT NULL,domain_attempt INT NOT NULL CHECK(domain_attempt>0),
 redrive_generation INT NOT NULL CHECK(redrive_generation>=0),
 effect_receipt_id BINARY(16) NULL,execution_receipt_id BINARY(16) NULL,execution_kind VARBINARY(40) NULL,
 payload LONGBLOB NOT NULL CHECK(OCTET_LENGTH(payload)<=67108864 AND JSON_VALID(CONVERT(payload USING utf8mb4))),payload_hash BINARY(32) NOT NULL,
 outcome VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'unknown' CHECK(outcome IN('unknown','accepted','rejected')),
 provider_reference VARCHAR(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL DEFAULT '',
 created_at DATETIME(6) NOT NULL,resolved_at DATETIME(6) NULL,
 CHECK(OCTET_LENGTH(provider_reference)<=512 AND LOCATE(X'00',CAST(provider_reference AS BINARY))=0),
 UNIQUE KEY de_dispatch_identity(tenant_id,kind,resource_id,domain_attempt,redrive_generation),
 CHECK((outcome='unknown' AND resolved_at IS NULL AND OCTET_LENGTH(provider_reference)=0) OR(outcome<>'unknown' AND resolved_at IS NOT NULL AND OCTET_LENGTH(provider_reference)>0)),
 CHECK((kind='async_decision_execution_callback' AND execution_kind=CAST(kind AS BINARY) AND execution_kind IS NOT NULL AND execution_receipt_id=receipt_id AND execution_receipt_id IS NOT NULL AND effect_receipt_id IS NULL)
 OR(kind IN('workflow_execution','screening_execution','scoring_request','outbox_event') AND effect_receipt_id=receipt_id AND effect_receipt_id IS NOT NULL AND execution_receipt_id IS NULL AND execution_kind IS NULL)),
 FOREIGN KEY(tenant_id,kind,effect_receipt_id) REFERENCES %s.decision_engine_effect_job_receipts(tenant_id,kind,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,execution_kind,execution_receipt_id) REFERENCES %s.decision_engine_execution_job_receipts(tenant_id,kind,id) ON DELETE RESTRICT
 ) ENGINE=InnoDB`, schema, table, schema, schema))
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
		switch event {
		case "INSERT":
			body += "IF NOT(NEW.payload_hash<=>UNHEX(SHA2(NEW.payload,256))) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='dispatch payload hash mismatch'; END IF; "
			body += fmt.Sprintf("IF NEW.outcome<>'unknown' OR NOT EXISTS(SELECT 1 FROM %s.decision_engine_effect_job_receipts r WHERE r.id=NEW.effect_receipt_id AND r.tenant_id=NEW.tenant_id AND r.kind=NEW.kind AND r.resource_id=NEW.resource_id AND r.domain_attempt=NEW.domain_attempt AND r.redrive_generation=NEW.redrive_generation) AND NOT EXISTS(SELECT 1 FROM %s.decision_engine_execution_job_receipts r WHERE r.id=NEW.execution_receipt_id AND r.tenant_id=NEW.tenant_id AND r.kind=NEW.kind AND r.resource_id=NEW.resource_id AND r.domain_attempt=NEW.domain_attempt AND r.redrive_generation=NEW.redrive_generation) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='dispatch receipt identity mismatch'; END IF; ", schema, schema)
		case "UPDATE":
			for _, column := range []string{"receipt_id", "tenant_id", "kind", "resource_id", "domain_attempt", "redrive_generation", "effect_receipt_id", "execution_receipt_id", "execution_kind", "payload", "payload_hash", "created_at"} {
				body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable dispatch intent'; END IF; ", column, column)
			}
			body += "IF OLD.outcome<>'unknown' OR NEW.outcome='unknown' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='terminal dispatch receipt is immutable'; END IF; "
		case "DELETE":
			body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='permanent dispatch intent'; "
		}
		name := table + "_" + strings.ToLower(event)
		add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body))
	}
	return steps, nil
}

func DispatchPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := DispatchStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindExecutionReference(database, steps, reference)
}
func DispatchDefinerGrants(database, user string) ([]string, error) {
	grants, err := EffectCancellationDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	return append(grants, "GRANT SELECT,TRIGGER ON "+schema+".decision_engine_dispatch_intents TO "+account+"@`localhost`"), nil
}
