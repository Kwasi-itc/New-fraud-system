// Package metadata contains target data-model schema definitions. They are not
// activated until the migration runner, owner protocols and grants are complete.
package metadata

import (
	"embed"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/exactkeys"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

//go:embed sql/*.sql
var source embed.FS

type keyBinding struct{ column, key, domain, scope, absent string }
type owner struct {
	table     string
	immutable []string
	keys      []keyBinding
}

// Definitions includes structural constraints and exact-key/identity guards.
// Publication, display validation/versioning, DDL intent and permission
// installation remain owner-protocol gates; never run this from app startup.
func Definitions(database string) ([]string, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	statements, err := exactkeys.Definitions(database)
	if err != nil {
		return nil, err
	}
	files, err := source.ReadDir("sql")
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		data, err := source.ReadFile("sql/" + file.Name())
		if err != nil {
			return nil, err
		}
		statements = append(statements, strings.ReplaceAll(string(data), "{{database}}", schema))
	}
	owners := []owner{
		{"data_model_schema_change_steps", []string{"tenant_id", "table_id", "operation_id", "step_no", "checksum", "started_at"}, nil},
		{"data_model_physical_tables", []string{"tenant_id", "table_id", "tenant_label", "table_label"}, nil},
		{"data_model_physical_fields", []string{"tenant_id", "table_id", "field_id", "component", "field_label"}, nil},
		{"data_model_schema_change_intents", []string{"tenant_id", "table_id", "operation_id", "request_hash", "actor_kind", "actor_id", "expected_epoch", "expected_version", "desired_shape", "created_at"}, nil},
		{"data_model_display_field_references", []string{"tenant_id", "table_id", "field_id"}, nil},
		{"data_model_display_link_references", []string{"tenant_id", "table_id", "link_id"}, nil},
		{"data_model_display_table_references", []string{"tenant_id", "table_id", "parent_table_id"}, nil},
		{"data_model_index_deliveries", []string{"id", "tenant_id", "job_id", "kind", "payload_version", "queue_name", "created_at", "max_attempts"}, nil},
		{"data_model_index_delivery_reservations", []string{"tenant_id", "job_id", "delivery_id"}, nil},
		{"data_model_tenants", []string{"id"}, []keyBinding{{"external_key", "external_key_id", "dm.tenant.external_key", "_binary''", "NEW.external_key IS NULL"}, {"schema_name", "schema_name_key_id", "dm.tenant.schema_name", "_binary''", ""}}},
		{"data_model_tables", []string{"id", "tenant_id"}, []keyBinding{{"name", "name_key_id", "dm.table.name", "NEW.tenant_id", ""}}},
		{"data_model_fields", []string{"id", "tenant_id", "table_id"}, []keyBinding{{"name", "name_key_id", "dm.field.name", "CONCAT(NEW.tenant_id,NEW.table_id)", ""}}},
		{"data_model_field_enum_values", []string{"id", "tenant_id", "field_id"}, []keyBinding{{"value", "value_key_id", "dm.enum.value", "CONCAT(NEW.tenant_id,NEW.field_id)", ""}, {"label", "label_key_id", "dm.enum.label", "CONCAT(NEW.tenant_id,NEW.field_id)", ""}}},
		{"data_model_links", []string{"id", "tenant_id", "parent_table_id", "parent_field_id", "child_table_id", "child_field_id"}, []keyBinding{{"name", "name_key_id", "dm.link.name", "CONCAT(NEW.tenant_id,NEW.child_table_id)", ""}}},
		{"data_model_pivots", []string{"id", "tenant_id", "base_table_id", "field_id"}, nil},
		{"data_model_pivot_path_links", []string{"tenant_id", "pivot_id", "ordinal", "link_id"}, nil},
		{"data_model_table_options", []string{"id", "tenant_id", "table_id"}, nil},
		{"data_model_displayed_fields", []string{"tenant_id", "table_id", "ordinal", "field_id"}, nil},
		{"data_model_field_order", []string{"tenant_id", "table_id", "ordinal", "field_id"}, nil},
		{"data_model_navigation_options", []string{"id", "tenant_id", "source_table_id", "source_field_id", "target_table_id", "filter_field_id", "ordering_field_id"}, nil},
		{"data_model_index_jobs", []string{"id", "tenant_id", "table_id", "table_name", "index_type", "columns_json", "dedupe_key"}, []keyBinding{{"dedupe_key", "dedupe_key_id", "dm.index.dedupe", "_binary''", "OCTET_LENGTH(NEW.dedupe_key)=0"}}},
		{"data_model_schema_change_log", []string{"id", "tenant_id", "operation", "resource_type", "resource_id", "status", "details", "created_at"}, nil},
		{"data_model_tenant_schema_migrations", []string{"id", "tenant_id", "version", "applied_at"}, nil},
		{"data_model_ingestion_contracts", []string{"tenant_id", "table_id"}, []keyBinding{{"table_name", "table_name_key_id", "dm.contract.name", "NEW.tenant_id", ""}}},
		{"data_model_publication_operations", []string{"tenant_id", "table_id", "operation_id", "request_hash", "condition_mode", "expected_version", "version", "payload"}, nil},
	}
	for _, o := range owners {
		for _, event := range []string{"INSERT", "UPDATE"} {
			var body strings.Builder
			body.WriteString("DECLARE v_key BINARY(16); IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='metadata integrity checks must be enabled'; END IF; ")
			var labels []string
			if o.table == "data_model_physical_tables" {
				labels = []string{"tenant_label", "table_label"}
			}
			if o.table == "data_model_physical_fields" {
				labels = []string{"field_label"}
			}
			for _, label := range labels {
				fmt.Fprintf(&body, "IF NOT REGEXP_LIKE(NEW.%s,'^[a-z0-9]+(_[a-z0-9]+)*$','c') OR REGEXP_LIKE(NEW.%s,'[^a-z0-9_]','c') THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid physical name label'; END IF; ", label, label)
			}
			if event == "UPDATE" {
				for _, column := range o.immutable {
					fmt.Fprintf(&body, "IF NOT (BINARY NEW.%s <=> BINARY OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable metadata identity or event'; END IF; ", column, column)
				}
			}
			for _, k := range o.keys {
				if k.absent != "" {
					fmt.Fprintf(&body, "IF %s THEN SET NEW.%s=NULL; ELSE ", k.absent, k.key)
				}
				fmt.Fprintf(&body, "CALL %s.data_model_resolve_exact_key(_binary'%s',%s,CAST(NEW.%s AS BINARY),v_key); SET NEW.%s=v_key; ", schema, k.domain, k.scope, k.column, k.key)
				if k.absent != "" {
					body.WriteString("END IF; ")
				}
			}
			statements = append(statements, fmt.Sprintf("CREATE TRIGGER %s.%s_guard_%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", schema, o.table, strings.ToLower(event), event, schema, o.table, body.String()))
		}
	}
	return statements, nil
}
