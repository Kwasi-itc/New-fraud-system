package postgres

import "testing"

func TestPoolConfigPreservesDefaultsAndAppliesExplicitLimits(t *testing.T) {
	const url = "postgres://example/test?pool_max_conns=8&pool_min_conns=2"
	for _, tc := range []struct {
		name     string
		limits   PoolConfig
		max, min int32
	}{
		{"defaults", PoolConfig{}, 8, 2},
		{"explicit", PoolConfig{MaxConns: 24, MinConns: 4}, 24, 4},
		{"only_max", PoolConfig{MaxConns: 12}, 12, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := poolConfig(url, tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxConns != tc.max || cfg.MinConns != tc.min {
				t.Fatalf("pool limits = %d/%d, want %d/%d", cfg.MaxConns, cfg.MinConns, tc.max, tc.min)
			}
		})
	}
}

func TestPoolConfigRejectsInvalidEffectiveLimits(t *testing.T) {
	for _, limits := range []PoolConfig{{MaxConns: -1}, {MinConns: -1}, {MaxConns: 1}, {MinConns: 9}} {
		if _, err := poolConfig("postgres://example/test?pool_max_conns=8&pool_min_conns=2", limits); err == nil {
			t.Fatalf("expected invalid limits %v to fail", limits)
		}
	}
}
