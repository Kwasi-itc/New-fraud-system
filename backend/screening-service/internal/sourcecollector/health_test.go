package sourcecollector

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOperationalHealthDoesNotMistakeMissingSignalsForHealthy(t *testing.T) {
	baseline := map[string]any{"collection_enabled": 1, "enabled_not_ready": 0, "quarantined": 0, "expired_source_leases": 0, "rescreen_retry_exhausted": 0, "source_retry_exhausted": 0, "rescreen_failed": 0, "oldest_publication_seconds": 0, "oldest_rescreen_seconds": 0}
	for _, tc := range []struct {
		name    string
		key     string
		value   any
		want    string
		invalid bool
	}{
		{name: "healthy", want: "healthy"}, {name: "inactive", key: "collection_enabled", value: 0, want: "inactive"},
		{name: "stale source", key: "enabled_not_ready", value: 1, want: "degraded"},
		{name: "quarantine", key: "quarantined", value: 1, want: "degraded"},
		{name: "expired lease", key: "expired_source_leases", value: 1, want: "degraded"},
		{name: "failed rescreen", key: "rescreen_failed", value: 1, want: "degraded"},
		{name: "exhausted source retry", key: "source_retry_exhausted", value: 1, want: "degraded"},
		{name: "exhausted rescreen retry", key: "rescreen_retry_exhausted", value: 1, want: "degraded"},
		{name: "old publication", key: "oldest_publication_seconds", value: 301, want: "degraded"},
		{name: "old rescreen", key: "oldest_rescreen_seconds", value: 301, want: "degraded"},
		{name: "at queue boundary", key: "oldest_rescreen_seconds", value: 300, want: "healthy"},
		{name: "null counter", key: "enabled_not_ready", value: nil, invalid: true},
		{name: "negative counter", key: "enabled_not_ready", value: -1, invalid: true},
		{name: "string counter", key: "enabled_not_ready", value: "0", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]any{}
			for k, v := range baseline {
				values[k] = v
			}
			if tc.key != "" {
				values[tc.key] = tc.value
			}
			raw, _ := json.Marshal(values)
			got, e := EvaluateOperationalHealth(raw, 5*time.Minute)
			if tc.invalid {
				if e == nil {
					t.Fatal("invalid signal accepted")
				}
				return
			}
			if e != nil || got.Status != tc.want {
				t.Fatalf("%+v %v", got, e)
			}
			if tc.want == "degraded" && len(got.Alerts) == 0 {
				t.Fatal("missing alert")
			}
		})
	}
	for _, raw := range []string{"{}", "[]", "not json"} {
		if _, e := EvaluateOperationalHealth([]byte(raw), 5*time.Minute); e == nil {
			t.Fatal("incomplete input accepted", raw)
		}
	}
	for _, threshold := range []time.Duration{0, 29 * time.Second, 25 * time.Hour} {
		raw, _ := json.Marshal(baseline)
		if _, e := EvaluateOperationalHealth(raw, threshold); e == nil {
			t.Fatal("unbounded alert threshold")
		}
	}
}
