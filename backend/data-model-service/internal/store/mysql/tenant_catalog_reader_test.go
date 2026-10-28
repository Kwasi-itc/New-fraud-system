package mysql

import (
	"context"
	"database/sql"
	"testing"
)

func TestTenantCatalogReaderConfiguration(t *testing.T) {
	auth := CatalogAuthorizer(func(context.Context, *sql.Tx) error { return nil })
	for _, scenario := range []string{"nil_db", "bad_database", "no_authorizer", "zero_rows", "large_rows", "zero_bytes", "large_bytes"} {
		t.Run(scenario, func(t *testing.T) {
			db, database, rows, bytes, authorizer := &sql.DB{}, "fixture", 100, 4096, auth
			switch scenario {
			case "nil_db":
				db = nil
			case "bad_database":
				database = "bad-name"
			case "no_authorizer":
				authorizer = nil
			case "zero_rows":
				rows = 0
			case "large_rows":
				rows = 100001
			case "zero_bytes":
				bytes = 0
			case "large_bytes":
				bytes = 64<<20 + 1
			}
			if _, err := NewTenantCatalogReader(db, database, rows, bytes, authorizer); err == nil {
				t.Fatal("invalid catalogue configuration accepted")
			}
		})
	}
	var nilReader *TenantCatalogReader
	if values, err := nilReader.List(context.Background()); err == nil || values != nil {
		t.Fatal("nil reader accepted")
	}
}
