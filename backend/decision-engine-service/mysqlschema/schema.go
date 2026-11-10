// Package mysqlschema exposes the decision owner's reviewed DDL for shared
// database composition. It does not apply migrations or adopt target state.
package mysqlschema

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func Statements(database, definer string) ([]migrations.Step, error) {
	return metadata.DispatchStatements(database, definer)
}
func DefinerGrants(database, definer string) ([]string, error) {
	return metadata.DispatchDefinerGrants(database, definer)
}
