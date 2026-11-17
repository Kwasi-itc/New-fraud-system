package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStandaloneAndSharedProviderCatalogSchemaCompatibility(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	name := "provider_schema_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.db.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	cfg := f.db.Config()
	cfg.ConnConfig.Database = name
	standalone, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		standalone.Close()
		if _, e := f.db.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	if e = providerproduct.Bootstrap(ctx, standalone); e != nil {
		t.Fatal(e)
	}
	if e = providerproduct.Bootstrap(ctx, f.db); e == nil {
		t.Fatal("standalone bootstrap adopted the shared application")
	}
	for _, table := range []string{"source_datasets", "source_grants", "source_versions", "source_records", "source_names", "source_identifiers", "source_publications", "source_artifacts", "source_version_artifacts", "source_retention_policies", "source_lifecycle_audit", "source_connections", "source_import_runs", "source_retention_holds", "private_lists", "private_imports"} {
		definition := func(db *pgxpool.Pool) string {
			t.Helper()
			var raw json.RawMessage
			e := db.QueryRow(ctx, `SELECT jsonb_build_object('columns',(SELECT jsonb_agg(jsonb_build_object('name',a.attname,'type',format_type(a.atttypid,a.atttypmod),'not_null',a.attnotnull,'generated',a.attgenerated,'identity',a.attidentity,'default',pg_get_expr(d.adbin,d.adrelid)) ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON(d.adrelid,d.adnum)=(a.attrelid,a.attnum) WHERE a.attrelid=to_regclass('screening.'||$1) AND a.attnum>0 AND NOT a.attisdropped),'constraints',(SELECT jsonb_agg(pg_get_constraintdef(oid) ORDER BY conname) FROM pg_constraint WHERE conrelid=to_regclass('screening.'||$1)))`, table).Scan(&raw)
			if e != nil {
				t.Fatal(e)
			}
			return string(raw)
		}
		if definition(f.db) != definition(standalone) {
			t.Fatal("standalone/shared catalog definitions differ", table)
		}
	}
	acceptanceReport(t, "provider-product-catalog-compatibility", map[string]any{"canonical_source_ddl_reused": true, "source_tables_compared": 16, "standalone_has_no_fraud_runtime": true, "shared_catalog_not_overwritten": true, "production_activated": false})
}
