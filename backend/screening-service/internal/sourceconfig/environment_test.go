package sourceconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretFileConfigurationIsExplicitAndBounded(t *testing.T) {
	t.Setenv("SOURCE_DATABASE_URL", "")
	t.Setenv("SOURCE_DATABASE_URL_FILE", "")
	if value, e := ReadSecret("SOURCE_DATABASE_URL"); e != nil || value != "" {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "dsn.txt")
	if e := os.WriteFile(path, []byte("postgres://test-only\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SOURCE_DATABASE_URL_FILE", path)
	if value, e := ReadSecret("SOURCE_DATABASE_URL"); e != nil || value != "postgres://test-only" {
		t.Fatal(e)
	}
	t.Setenv("SOURCE_DATABASE_URL", "postgres://ambiguous")
	if _, e := ReadSecret("SOURCE_DATABASE_URL"); e == nil {
		t.Fatal("ambiguous secret sources")
	}
	t.Setenv("SOURCE_DATABASE_URL", "")
	if e := os.WriteFile(path, make([]byte, 8193), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadSecret("SOURCE_DATABASE_URL"); e == nil {
		t.Fatal("oversized secret")
	}
}
