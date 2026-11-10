// Package mysqlcase composes the complete case owner lineage, preserving the
// published access, workflow and review definitions as an exact prefix.
package mysqlcase

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlreview"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlschema"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func Statements(database, definer string) ([]migrations.Step, error) {
	prefix, e := mysqlreview.Statements(database, definer)
	if e != nil {
		return nil, e
	}
	suffix, e := mysqlschema.Statements(database, definer)
	if e != nil {
		return nil, e
	}
	return append(prefix, suffix...), nil
}
func DefinerGrants(database, definer string) ([]string, error) {
	prefix, e := mysqlreview.DefinerGrants(database, definer)
	if e != nil {
		return nil, e
	}
	suffix, e := mysqlschema.DefinerGrants(database, definer)
	if e != nil {
		return nil, e
	}
	return append(prefix, suffix...), nil
}
