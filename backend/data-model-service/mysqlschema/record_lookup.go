package mysqlschema

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// RecordLookupStatements exposes the owner's read-only exact-key contract for
// independent consumer fixtures. Production installation retains its v005
// prerequisite and definer checks in the data-model installer.
func RecordLookupStatements(database, definer string) ([]migrations.Step, error) {
	return installation.RecordLookupUpgradeStatements(database, installation.Definer{User: definer, Host: "localhost"})
}
