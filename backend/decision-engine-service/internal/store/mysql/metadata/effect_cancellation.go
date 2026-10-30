package metadata

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// EffectCancellationStatements appends to the unchanged 140-step execution
// lineage. Cancellation is a decision-owned dispatch outcome, not permission
// to rewrite a provider's result or claim an external action was undone.
func EffectCancellationStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := ExecutionStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	add := func(kind, name, statement string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: statement})
	}
	table := "decision_engine_effect_cancellations"
	add("TABLE", table, fmt.Sprintf(`CREATE TABLE %s.%s(
 receipt_id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,
 kind VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,resource_id BINARY(16) NOT NULL,
 domain_attempt INT NOT NULL CHECK(domain_attempt>0),redrive_generation INT NOT NULL CHECK(redrive_generation>=0),
 reason_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 cancelled_at DATETIME(6) NOT NULL,
 CHECK(OCTET_LENGTH(reason_code)>0),
 UNIQUE KEY de_effect_cancellation_identity(tenant_id,kind,resource_id,domain_attempt,redrive_generation),
 FOREIGN KEY(tenant_id,kind,receipt_id) REFERENCES %s.decision_engine_effect_job_receipts(tenant_id,kind,id) ON DELETE RESTRICT
 ) ENGINE=InnoDB`, schema, table, schema))
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
		if event == "INSERT" {
			body += "IF OCTET_LENGTH(NEW.reason_code)=0 OR REGEXP_LIKE(NEW.reason_code,'[^a-z0-9_]','c') THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='redacted cancellation reason required'; END IF; "
			body += fmt.Sprintf("IF NOT EXISTS(SELECT 1 FROM %s.decision_engine_effect_job_receipts r WHERE r.id=NEW.receipt_id AND r.tenant_id=NEW.tenant_id AND r.kind=NEW.kind AND r.resource_id=NEW.resource_id AND r.domain_attempt=NEW.domain_attempt AND r.redrive_generation=NEW.redrive_generation) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cancellation receipt identity mismatch'; END IF; ", schema)
		} else {
			body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable effect cancellation'; "
		}
		name := table + "_" + strings.ToLower(event)
		add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body))
	}
	// Permanent attempt reservations must not be deleted to bypass deduplication.
	name := "decision_engine_effect_job_receipts_delete"
	add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE DELETE ON %s.decision_engine_effect_job_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable effect receipt'", user, schema, name, schema))
	return steps, nil
}

func EffectCancellationPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := EffectCancellationStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindExecutionReference(database, steps, reference)
}

func EffectCancellationDefinerGrants(database, user string) ([]string, error) {
	grants, err := ExecutionDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	return append(grants, "GRANT SELECT,TRIGGER ON "+schema+".decision_engine_effect_cancellations TO "+account+"@`localhost`"), nil
}
