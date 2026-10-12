package app

import "testing"

func TestDataModelReadModeConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused/test")
	t.Setenv("SERVICE_AUTH_MODE", "disabled")
	for _, test := range []struct {
		name, mode, url, wantMode string
		wantError                 bool
	}{
		{"default SQL without HTTP dependency", "", "", "sql", false},
		{"explicit SQL", "sql", "", "sql", false},
		{"HTTP requires URL", "http", "", "", true},
		{"HTTP compatibility", "http", "http://unused", "http", false},
		{"reject unknown mode", "unknown", "http://unused", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DATA_MODEL_READ_MODE", test.mode)
			t.Setenv("DATA_MODEL_SERVICE_URL", test.url)
			cfg, err := LoadConfig()
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v", err)
			}
			if err == nil && cfg.DataModelReadMode != test.wantMode {
				t.Fatalf("mode=%s", cfg.DataModelReadMode)
			}
		})
	}
}

func TestDataModelCacheConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused/test")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://unused")
	t.Setenv("SERVICE_AUTH_MODE", "disabled")
	for _, test := range []struct {
		name, entries, bytes   string
		wantEntries, wantBytes int
		wantError              bool
	}{
		{"defaults", "", "", 128, 16 << 20, false},
		{"disabled", "0", "0", 0, 0, false},
		{"configured", "4", "1024", 4, 1024, false},
		{"negative entries", "-1", "1024", 0, 0, true},
		{"negative bytes", "4", "-1", 0, 0, true},
		{"invalid entries", "many", "1024", 0, 0, true},
		{"invalid bytes", "4", "16MB", 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DATA_MODEL_CACHE_MAX_ENTRIES", test.entries)
			t.Setenv("DATA_MODEL_CACHE_MAX_BYTES", test.bytes)
			cfg, err := LoadConfig()
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v", err)
			}
			if err == nil && (cfg.DataModelCacheMaxEntries != test.wantEntries || cfg.DataModelCacheMaxBytes != test.wantBytes) {
				t.Fatalf("cache configuration: %d, %d", cfg.DataModelCacheMaxEntries, cfg.DataModelCacheMaxBytes)
			}
		})
	}
}
