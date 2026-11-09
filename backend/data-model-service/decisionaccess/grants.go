package decisionaccess

import "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"

// PreparationPolicy grants metadata preparation only. Physical installation
// uses the separately reviewed installer policy, never these API credentials.
// Identity verification grants must be supplied by the identity owner.
func PreparationPolicy(user, host string) installation.AccountPolicy {
	p := installation.AccountPolicy{User: user, Host: host}
	for _, name := range []string{"tenants", "tables", "fields", "links", "pivots", "pivot_path_links", "navigation_options", "table_options", "displayed_fields", "field_order", "field_enum_values", "physical_tables", "tenant_schema_migrations"} {
		p.Objects = append(p.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "data_model_" + name, Privileges: []string{"SELECT"}})
	}
	for _, name := range []string{"index_jobs", "index_deliveries", "index_delivery_reservations"} {
		privileges := []string{"SELECT", "INSERT", "UPDATE"}
		if name == "index_delivery_reservations" {
			privileges = append(privileges, "DELETE")
		}
		p.Objects = append(p.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "data_model_" + name, Privileges: privileges})
	}
	p.Objects = append(p.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "data_model_schema_change_log", Privileges: []string{"INSERT"}})
	return p
}

// ExecutionPolicy is the metadata worker role. DDL remains delegated to the
// physical installer; this account cannot alter models or foreign owner data.
func ExecutionPolicy(user, host string) installation.AccountPolicy {
	return installation.AccountPolicy{User: user, Host: host, Objects: []installation.ObjectGrant{
		{Kind: "TABLE", Name: "data_model_index_jobs", Privileges: []string{"SELECT", "UPDATE"}},
		{Kind: "TABLE", Name: "data_model_index_deliveries", Privileges: []string{"SELECT", "UPDATE"}},
		{Kind: "TABLE", Name: "data_model_index_delivery_reservations", Privileges: []string{"SELECT", "DELETE"}},
		{Kind: "TABLE", Name: "data_model_schema_change_log", Privileges: []string{"INSERT"}},
	}}
}
