package installation

// PresentationOwnerPolicy permits table/field presentation and their atomic
// audit/revision/result receipts. It excludes lifecycle/enforcement columns,
// physical gates, dictionary, contract mutation and receipt update/delete.
// Tenant/actor authorization remains mandatory; grants are not row security.
func PresentationOwnerPolicy(principal Definer, locked bool) (AccountPolicy, error) {
	if _, err := principal.SQLAccount(); err != nil {
		return AccountPolicy{}, err
	}
	p := AccountPolicy{User: principal.User, Host: principal.Host, Locked: locked}
	for _, name := range []string{"data_model_tenants", "data_model_field_enum_values", "data_model_physical_tables", "data_model_ingestion_contracts"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT"}})
	}
	p.Objects = append(p.Objects,
		ObjectGrant{Kind: "TABLE", Name: "data_model_tables", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"description", "alias", "semantic_type", "updated_at"}}},
		ObjectGrant{Kind: "TABLE", Name: "data_model_fields", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"description", "alias", "updated_at"}}},
		ObjectGrant{Kind: "TABLE", Name: "data_model_index_jobs", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"status", "error_message", "started_at", "completed_at", "scheduled_at"}}},
		ObjectGrant{Kind: "TABLE", Name: "data_model_index_delivery_reservations", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"tenant_id"}}},
	)
	for _, name := range []string{"data_model_publication_operations", "data_model_owner_results", "data_model_index_deliveries", "data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT", "INSERT"}})
	}
	return p, nil
}
