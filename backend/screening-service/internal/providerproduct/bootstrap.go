// Package providerproduct owns the independently deployable provider boundary.
// Matching, normalization, collection and source DDL retain one implementation.
package providerproduct

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// catalogDDL reuses named, reviewed portions of the canonical screening owner
// migrations. It excludes workflow tables/triggers rather than creating fake
// core/identity/case tables. Missing/changed boundaries fail closed.
func catalogDDL() (string, error) {
	type section struct{ file, from, until string }
	sections := []section{
		{"000011_controlled_sources.up.sql", "", "ALTER TABLE screening.screenings"},
		{"000011_controlled_sources.up.sql", "CREATE TABLE screening.source_read_cursors", ""},
		{"000012_fuzzy_names.up.sql", "", ""},
		{"000013_source_lifecycle.up.sql", "CREATE TABLE screening.source_artifacts", ""},
		{"000014_source_connectors.up.sql", "", "CREATE TABLE screening.source_monitor_audit"},
		{"000015_private_lists.up.sql", "", "CREATE INDEX source_rescreen_monitor_status"},
		{"000018_fuzzy_read_latency.up.sql", "", ""},
		{"000019_reviewed_notices.up.sql", "", ""},
		{"000021_normalizer_revision.up.sql", "", ""},
		{"000022_source_statistics.up.sql", "", ""},
		{"000023_provider_clients.up.sql", "", ""},
	}
	var out strings.Builder
	out.WriteString("CREATE SCHEMA screening;\n")
	transaction := regexp.MustCompile(`(?m)^(BEGIN;|COMMIT;)[\t ]*\r?$`)
	for _, s := range sections {
		raw, e := migrations.Metadata.ReadFile("metadata/" + s.file)
		if e != nil {
			return "", e
		}
		sql := strings.ReplaceAll(string(raw), "\r\n", "\n")
		if s.from != "" {
			if strings.Count(sql, s.from) != 1 {
				return "", fmt.Errorf("catalog migration start changed: %s", s.file)
			}
			sql = sql[strings.Index(sql, s.from):]
		}
		if s.until != "" {
			if strings.Count(sql, s.until) != 1 {
				return "", fmt.Errorf("catalog migration end changed: %s", s.file)
			}
			sql = sql[:strings.Index(sql, s.until)]
		}
		out.WriteString(transaction.ReplaceAllString(sql, ""))
		out.WriteByte('\n')
	}
	return out.String(), nil
}

// Bootstrap installs only into an empty database, or verifies this exact prior
// standalone bootstrap. Shared installations use the platform owner migrator.
func Bootstrap(ctx context.Context, db *pgxpool.Pool) error {
	sql, e := catalogDDL()
	if e != nil {
		return e
	}
	sum := sha256.Sum256([]byte(sql))
	digest := hex.EncodeToString(sum[:])
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='60s'; SELECT pg_advisory_xact_lock(1800415344,23)`); e != nil {
		return e
	}
	var present bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('provider_api.installation') IS NOT NULL`).Scan(&present); e != nil {
		return e
	}
	if present {
		var topology, installed string
		var version int
		if e = tx.QueryRow(ctx, `SELECT topology,bootstrap_sha256,contract_version FROM provider_api.installation WHERE singleton`).Scan(&topology, &installed, &version); e != nil {
			return e
		}
		if topology != "standalone" || installed != digest || version != 1 {
			return errors.New("existing database is not this standalone catalog; use its owner upgrade procedure")
		}
		return tx.Commit(ctx)
	}
	var occupied bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname IN('core','identity','screening','case_manager','provider_api')) OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p','v','m','S'))`).Scan(&occupied); e != nil {
		return e
	}
	if occupied {
		return errors.New("standalone bootstrap requires an empty database; shared catalogs use screening migration 23")
	}
	if _, e = tx.Exec(ctx, sql); e != nil {
		return fmt.Errorf("standalone catalog migration: %w", e)
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_api.installation SET topology='standalone',bootstrap_sha256=$1 WHERE singleton`, digest); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func Topology(ctx context.Context, db *pgxpool.Pool) (string, error) {
	var topology string
	var version int
	if e := db.QueryRow(ctx, `SELECT topology,contract_version FROM provider_api.installation WHERE singleton`).Scan(&topology, &version); e != nil || version != 1 || (topology != "shared" && topology != "standalone") {
		return "", errors.New("provider catalog contract unavailable; run the appropriate owner migration")
	}
	return topology, nil
}
