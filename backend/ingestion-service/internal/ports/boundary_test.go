package ports

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Regressions here would couple the next MySQL adapter to a pgx transaction or
// transport client again. Driver/queue implementation details belong in stores
// and runtime wiring, not service mutation ports or domain business logic.
func TestDomainBoundaryDoesNotImportDrivers(t *testing.T) {
	for _, dir := range []string{".", "../service"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if path == "database/sql" || strings.HasPrefix(path, "github.com/jackc/pgx") || strings.HasPrefix(path, "github.com/riverqueue/") || strings.HasSuffix(path, "/internal/riverjobs") {
					t.Errorf("%s imports transport/driver %s", filepath.Join(dir, entry.Name()), path)
				}
			}
		}
	}
}
