package installation

// OptionsOwnerPolicy permits ordered option replacement and atomic evidence.
// It cannot edit display JSON, caption dependencies, field semantics or gates.
// Tenant/actor authorization remains an owner responsibility.
func OptionsOwnerPolicy(principal Definer, locked bool) (AccountPolicy, error) {
	if _, err := principal.SQLAccount(); err != nil {
		return AccountPolicy{}, err
	}
	p := AccountPolicy{User: principal.User, Host: principal.Host, Locked: locked}
	for _, name := range []string{"data_model_tenants", "data_model_fields", "data_model_field_enum_values", "data_model_physical_tables", "data_model_ingestion_contracts"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT"}})
	}
	p.Objects = append(p.Objects,
		ObjectGrant{Kind: "TABLE", Name: "data_model_tables", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"display_version"}}},
		ObjectGrant{Kind: "TABLE", Name: "data_model_table_options", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"updated_at"}}},
	)
	for _, name := range []string{"data_model_displayed_fields", "data_model_field_order"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT", "INSERT", "DELETE"}})
	}
	for _, name := range []string{"data_model_publication_operations", "data_model_owner_results", "data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT", "INSERT"}})
	}
	return p, nil
}
