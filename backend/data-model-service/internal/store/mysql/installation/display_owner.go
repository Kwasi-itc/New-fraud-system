package installation

// DisplayResultOwnerPolicy adds immutable original-result insertion and reads.
// It requires the v004 owner-result installation and its locked guard definer.
func DisplayResultOwnerPolicy(principal Definer, locked bool) (AccountPolicy, error) {
	p, err := DisplayOwnerPolicy(principal, locked)
	if err != nil {
		return AccountPolicy{}, err
	}
	p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: "data_model_owner_results", Privileges: []string{"SELECT", "INSERT"}})
	return p, nil
}

// DisplayOwnerPolicy is the staged display-save/caption-job capability. It has
// no physical DDL, dictionary, ingestion-contract mutation, gate mutation or
// receipt update/delete rights. Tenant/actor authorization is still required:
// object grants are not row-level security. Compose and review the complete
// deployment account separately; this helper does not grant or unlock accounts.
func DisplayOwnerPolicy(principal Definer, locked bool) (AccountPolicy, error) {
	if _, err := principal.SQLAccount(); err != nil {
		return AccountPolicy{}, err
	}
	p := AccountPolicy{User: principal.User, Host: principal.Host, Locked: locked}
	for _, name := range []string{"data_model_tenants", "data_model_fields", "data_model_field_enum_values", "data_model_links", "data_model_physical_tables", "data_model_ingestion_contracts"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT"}})
	}
	p.Objects = append(p.Objects,
		ObjectGrant{Kind: "TABLE", Name: "data_model_tables", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"caption_field", "display_version", "updated_at"}}},
		ObjectGrant{Kind: "TABLE", Name: "data_model_table_options", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"updated_at", "field_display", "display_columns"}}},
		ObjectGrant{Kind: "TABLE", Name: "data_model_index_jobs", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"status", "error_message", "started_at", "completed_at", "scheduled_at"}}},
	)
	for _, name := range []string{"data_model_displayed_fields", "data_model_field_order", "data_model_display_field_references", "data_model_display_link_references", "data_model_display_table_references"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT", "INSERT", "DELETE"}})
	}
	// MySQL locking reads require UPDATE or DELETE in addition to SELECT.
	// Permit only the immutable tenant identity for reservation locking; the
	// installed identity guard rejects changes, and no delivery lifecycle UPDATE
	// or reservation DELETE capability is granted to this producer.
	p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: "data_model_index_delivery_reservations", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"tenant_id"}}})
	for _, name := range []string{"data_model_publication_operations", "data_model_index_deliveries", "data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
		p.Objects = append(p.Objects, ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT", "INSERT"}})
	}
	return p, nil
}
