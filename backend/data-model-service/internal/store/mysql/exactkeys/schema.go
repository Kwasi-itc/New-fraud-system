// Package exactkeys defines the data-model owner's private exact-byte key
// dictionary. Definitions are migration inputs, not runtime schema installers.
package exactkeys

import (
	"embed"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// Each file contains exactly one server statement, including complete routine
// bodies. Do not split routines on semicolons or enable driver multiStatements.
//
//go:embed sql/*.sql
var source embed.FS

// Definitions returns ordered, schema-qualified statements for a new lineage.
// The privileged migration runner must track checksums/recovery and install
// restricted definers/grants. No IF NOT EXISTS masks an incompatible object.
func Definitions(database string) ([]string, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	files, err := source.ReadDir("sql")
	if err != nil {
		return nil, err
	}
	statements := make([]string, 0, len(files))
	for _, file := range files {
		data, err := source.ReadFile("sql/" + file.Name())
		if err != nil {
			return nil, err
		}
		statements = append(statements, strings.ReplaceAll(string(data), "{{database}}", schema))
	}
	return statements, nil
}
