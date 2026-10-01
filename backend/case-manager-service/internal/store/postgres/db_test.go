package postgres

import "testing"

func TestSharedDatabasePoolLimits(t *testing.T) {
	for _, name := range []string{"DB_POOL_MAX_CONNS", "DB_STATEMENT_TIMEOUT", "DB_LOCK_TIMEOUT", "DB_IDLE_TRANSACTION_TIMEOUT"} {
		t.Setenv(name, "")
	}
	cfg, err := poolConfig("postgres://localhost/test?pool_max_conns=99")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 8 || cfg.MinConns != 0 || cfg.ConnConfig.RuntimeParams["lock_timeout"] != "3000" {
		t.Fatalf("unexpected pool configuration: %+v", cfg)
	}
	t.Setenv("DB_POOL_MAX_CONNS", "4")
	t.Setenv("DB_STATEMENT_TIMEOUT", "2s")
	cfg, err = poolConfig("postgres://localhost/test")
	if err != nil || cfg.MaxConns != 4 || cfg.ConnConfig.RuntimeParams["statement_timeout"] != "2000" {
		t.Fatalf("overrides: %v", err)
	}
	for _, raw := range []string{"0", "-1", "101", "invalid"} {
		t.Setenv("DB_POOL_MAX_CONNS", raw)
		if _, err := poolConfig("postgres://localhost/test"); err == nil {
			t.Fatalf("invalid pool limit %s accepted", raw)
		}
	}
	t.Setenv("DB_POOL_MAX_CONNS", "4")
	t.Setenv("DB_LOCK_TIMEOUT", "0s")
	if _, err := poolConfig("postgres://localhost/test"); err == nil {
		t.Fatal("unbounded lock timeout accepted")
	}
}
