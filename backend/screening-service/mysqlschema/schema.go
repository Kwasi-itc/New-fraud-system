// Package mysqlschema owns the staged screening publication schema. It is
// deliberately separate from the PostgreSQL application startup path.
package mysqlschema

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// Statements requires the decision owner's effects migration in the same
// logical database. Raw provider responses are never exposed in the read view.
// Linked publications require an explicit execution binding; unbound legacy
// rows must be resolved by migration tooling before publication.
func Statements(database, definer string) ([]migrations.Step, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(definer)
	if err != nil {
		return nil, err
	}
	var steps []migrations.Step
	add := func(kind, name, sql string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("screening_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: sql})
	}
	add("TABLE", "screening_screenings", fmt.Sprintf(`CREATE TABLE %s.screening_screenings (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL,
 decision_id BINARY(16) NULL, scenario_id BINARY(16) NULL, screening_config_id BINARY(16) NULL, execution_id BINARY(16) NULL,
 projection_version BIGINT NOT NULL DEFAULT 1 CHECK(projection_version>0),
 status VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL CHECK(status IN('pending','queued','processing','awaiting_review','confirmed_hit','no_hit','archived','failed')),
 provider VARCHAR(128) NOT NULL, object_type VARCHAR(128) NOT NULL, object_id VARCHAR(512) NOT NULL,
 request_json JSON NOT NULL, response_json JSON NOT NULL,
 decision_projection JSON NOT NULL CHECK(JSON_TYPE(decision_projection)='OBJECT' AND OCTET_LENGTH(decision_projection)<=1048576),
 provider_reference TEXT NOT NULL CHECK(OCTET_LENGTH(provider_reference)<=16384), last_error TEXT NOT NULL CHECK(OCTET_LENGTH(last_error)<=16384),
 partial BOOLEAN NOT NULL DEFAULT FALSE CHECK(partial IN(0,1)),
 match_count INT NOT NULL DEFAULT 0 CHECK(match_count BETWEEN 0 AND 500),
 created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
 sent_at DATETIME(6) NULL, completed_at DATETIME(6) NULL, failed_at DATETIME(6) NULL,
 UNIQUE KEY screening_tenant_id(tenant_id,id), UNIQUE KEY screening_execution(tenant_id,execution_id),
 KEY screening_decision(tenant_id,decision_id,created_at,id),
 CHECK((decision_id IS NULL AND scenario_id IS NULL AND screening_config_id IS NULL AND execution_id IS NULL) OR
 (decision_id IS NOT NULL AND scenario_id IS NOT NULL AND screening_config_id IS NOT NULL AND execution_id IS NOT NULL)),
 FOREIGN KEY(tenant_id,decision_id) REFERENCES %s.decision_engine_decisions(tenant_id,id),
 FOREIGN KEY(tenant_id,scenario_id,screening_config_id) REFERENCES %s.decision_engine_screening_configs(tenant_id,scenario_id,id),
 FOREIGN KEY(tenant_id,execution_id) REFERENCES %s.decision_engine_screening_executions(tenant_id,id)
 ) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, s, s, s, s))
	add("TABLE", "screening_execution_projection_queue", fmt.Sprintf(`CREATE TABLE %s.screening_execution_projection_queue(
 tenant_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,version BIGINT NOT NULL CHECK(version>0),
 attempts INT NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 1000000),available_at DATETIME(6) NOT NULL,
 last_error VARCHAR(2000) NOT NULL DEFAULT '',updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,screening_id),KEY screening_projection_ready(available_at,tenant_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES %s.screening_screenings(tenant_id,id) ON DELETE CASCADE
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, s, s))
	for _, event := range []string{"INSERT", "UPDATE"} {
		body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening integrity checks required'; END IF; "
		if event == "INSERT" {
			body += fmt.Sprintf(`IF NEW.execution_id IS NOT NULL THEN
    SELECT COUNT(*) INTO bound FROM %s.decision_engine_screening_executions
    WHERE tenant_id=NEW.tenant_id AND id=NEW.execution_id AND decision_id=NEW.decision_id
    AND scenario_id=NEW.scenario_id AND config_id=NEW.screening_config_id FOR SHARE;
    IF bound<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening execution binding mismatch'; END IF;
   END IF; `, s)
		} else {
			for _, col := range []string{"id", "tenant_id", "decision_id", "scenario_id", "screening_config_id", "execution_id", "created_at"} {
				body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable screening binding'; END IF; ", col, col)
			}
			body += "IF NEW.projection_version<>OLD.projection_version+1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening publication requires next version'; END IF; "
		}
		body += `SET NEW.decision_projection=JSON_OBJECT(
 'screening_id',BIN_TO_UUID(NEW.id),'decision_id',BIN_TO_UUID(NEW.decision_id),
 'scenario_id',BIN_TO_UUID(NEW.scenario_id),'screening_config_id',BIN_TO_UUID(NEW.screening_config_id),
 'status',NEW.status,'provider',NEW.provider,'object_type',NEW.object_type,'object_id',NEW.object_id,
 'provider_reference',NEW.provider_reference,'last_error',NEW.last_error,'partial',JSON_EXTRACT(IF(NEW.partial,'true','false'),'$'),
 'idempotency_key',COALESCE(BIN_TO_UUID(NEW.execution_id),''),'match_count',NEW.match_count,
 'completed_at',NEW.completed_at,'source_version',NEW.projection_version); `
		name := "screening_publication_" + strings.ToLower(event)
		add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.screening_screenings FOR EACH ROW BEGIN DECLARE bound INT DEFAULT 0; %s END", u, s, name, event, s, body))
		name = "screening_projection_enqueue_" + strings.ToLower(event)
		add("TRIGGER", name, fmt.Sprintf(`CREATE DEFINER=%s@%s TRIGGER %s.%s AFTER %s ON %s.screening_screenings FOR EACH ROW BEGIN
 IF NEW.execution_id IS NOT NULL THEN
 INSERT INTO %s.screening_execution_projection_queue(tenant_id,screening_id,version,available_at,updated_at)
 VALUES(NEW.tenant_id,NEW.id,NEW.projection_version,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
 ON DUPLICATE KEY UPDATE version=NEW.projection_version,attempts=0,last_error='',available_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6);
 END IF; END`, u, "`localhost`", s, name, event, s, s))
	}
	add("VIEW", "screening_decision_source_v1", fmt.Sprintf(`CREATE ALGORITHM=MERGE DEFINER=%s@%s SQL SECURITY INVOKER VIEW %s.screening_decision_source_v1 AS
 SELECT id,tenant_id,decision_id,scenario_id,screening_config_id AS config_id,execution_id,projection_version,status,
 updated_at,sent_at,completed_at,failed_at,decision_projection AS response_json,provider_reference,last_error
 FROM %s.screening_screenings WHERE execution_id IS NOT NULL WITH CASCADED CHECK OPTION`, u, "`localhost`", s, s))
	// Append-only owner lineage: match evidence is scoped by its screening and
	// bounded by the publication repository before any replacement is written.
	add("TABLE", "screening_matches", fmt.Sprintf(`CREATE TABLE %s.screening_matches(
 tenant_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,
 entity_id VARCHAR(512) NOT NULL,provider VARCHAR(128) NOT NULL,name VARCHAR(1000) NOT NULL,
 status VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL CHECK(status IN('pending','confirmed_hit','no_hit','skipped')),
 score DOUBLE NOT NULL,payload JSON NOT NULL CHECK(OCTET_LENGTH(payload)<=1048576),
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,screening_id,id),UNIQUE KEY screening_match_identity(tenant_id,id),
 KEY screening_match_review(tenant_id,screening_id,status),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES %s.screening_screenings(tenant_id,id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, s, s))
	add("TABLE", "screening_execution_projection_queue", fmt.Sprintf("ALTER TABLE %s.screening_execution_projection_queue ADD KEY screening_projection_tenant_ready(tenant_id,available_at,screening_id)", s))
	intakeStatements(s, u, add)
	add("TABLE", "screening_screenings", fmt.Sprintf("ALTER TABLE %s.screening_screenings ADD KEY screening_local_ready(tenant_id,provider,status,created_at,id)", s))
	return steps, nil
}

// Plan accepts only exact definitions captured in a disposable reference
// database. Deployment must not adopt the destination's observed state.
func Plan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := Statements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(steps) != len(reference) {
		return migrations.Plan{}, fmt.Errorf("screening reference length mismatch")
	}
	seen := map[string]bool{}
	for i, s := range steps {
		r := reference[i]
		key := s.Kind + ":" + s.Name
		if r.ID != s.ID || r.Kind != s.Kind || r.Name != s.Name || r.SQL != s.SQL || !seen[key] && r.Before != "" {
			return migrations.Plan{}, fmt.Errorf("screening reference mismatch at %d", i+1)
		}
		steps[i].After = r.After
		steps[i].Before = r.Before
		seen[key] = true
	}
	p := migrations.Plan{Database: database, Owner: "screening", Steps: steps}
	return p, p.Validate()
}

func DefinerGrants(database, definer string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(definer)
	if err != nil {
		return nil, err
	}
	return []string{
		"GRANT SELECT,TRIGGER ON " + s + ".screening_screenings TO " + u + "@`localhost`",
		"GRANT UPDATE(decision_projection) ON " + s + ".screening_screenings TO " + u + "@`localhost`",
		"GRANT SELECT ON " + s + ".decision_engine_screening_executions TO " + u + "@`localhost`",
		"GRANT SELECT,INSERT,UPDATE ON " + s + ".screening_execution_projection_queue TO " + u + "@`localhost`",
		"GRANT SELECT,TRIGGER ON " + s + ".screening_intake_keys TO " + u + "@`localhost`",
		"GRANT SELECT,TRIGGER ON " + s + ".screening_intake_receipts TO " + u + "@`localhost`",
	}, nil
}
