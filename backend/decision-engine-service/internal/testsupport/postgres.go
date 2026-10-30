// Package testsupport owns local, disposable fixtures. Application code must
// not import it; no fixture is permitted to reset an application database.
package testsupport

import (
	"context"
	neturl "net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDatabaseURL creates a new database, never resets the supplied admin
// database, and only drops the generated database during cleanup.
func PostgresDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DECISION_ENGINE_TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("DECISION_ENGINE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("required disposable decision fixture is absent")
		}
		t.Skip("set DECISION_ENGINE_TEST_DATABASE_URL with CREATE DATABASE permission; each test uses a disposable database")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	local := func(host string) bool { return host == "postgres" || host == "localhost" || host == "127.0.0.1" }
	if !local(config.ConnConfig.Host) || !strings.HasPrefix(config.ConnConfig.Database, "decision_test_") {
		t.Fatal("decision tests require a local disposable decision_test_* admin database")
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if !local(fallback.Host) {
			t.Fatal("decision fixture may not fall back to an external host")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	name := "decision_test_" + uuid.NewString()
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, `DROP DATABASE "`+name+`" WITH (FORCE)`); err != nil {
			t.Errorf("drop disposable test database: %v", err)
		}
	})
	// ConnString retains the original input even after Database changes.
	parsed, err := neturl.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		parsed.Path, parsed.RawPath = "/"+name, ""
		query := parsed.Query()
		query.Del("dbname")
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return dsn + " dbname='" + name + "'"
}
