// Package mysqlschema exposes the data-model owner's reviewed migration builder
// to shared-database composition. It does not inspect or migrate a destination.
package mysqlschema

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func Statements(database, definer string) ([]migrations.Step, error) {
	return installation.Statements(database, installation.Definer{User: definer, Host: "localhost"})
}
func Plan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	return installation.Plan(database, installation.Definer{User: definer, Host: "localhost"}, reference)
}
func DefinerGrants(database, definer string) ([]string, error) {
	return installation.DefinerGrants(database, installation.Definer{User: definer, Host: "localhost"})
}
