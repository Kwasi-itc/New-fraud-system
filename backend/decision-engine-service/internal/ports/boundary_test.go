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

func TestDecisionMutationBoundaryHasNoDrivers(t *testing.T) {
	for _, dir := range []string{".", "../service"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, i := range f.Imports {
				p, err := strconv.Unquote(i.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if p == "database/sql" || strings.HasPrefix(p, "github.com/jackc/pgx") || strings.HasPrefix(p, "github.com/riverqueue/") || strings.HasSuffix(p, "/internal/riverjobs") {
					t.Errorf("%s imports driver/transport %s", entry.Name(), p)
				}
			}
		}
	}
}
