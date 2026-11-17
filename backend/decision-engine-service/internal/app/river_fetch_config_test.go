package app

import (
	"testing"
	"time"
)

func TestRiverFetchTimingBoundsAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		cooldown, poll string
		valid          bool
	}{{"", "", true}, {"5ms", "100ms", true}, {"100ms", "1s", true}, {"1ms", "1s", false}, {"0s", "1s", false}, {"1s", "100ms", false}, {"100ms", "2m", false}, {"invalid", "1s", false}} {
		t.Run(tc.cooldown+"/"+tc.poll, func(t *testing.T) {
			setRequiredConfigEnv(t)
			t.Setenv("RIVER_FETCH_COOLDOWN", tc.cooldown)
			t.Setenv("RIVER_FETCH_POLL_INTERVAL", tc.poll)
			cfg, e := LoadConfig()
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, e)
			}
			if tc.valid && tc.cooldown == "" && (cfg.RiverFetchCooldown != 100*time.Millisecond || cfg.RiverFetchPollInterval != time.Second) {
				t.Fatal("unexpected River defaults")
			}
		})
	}
}
