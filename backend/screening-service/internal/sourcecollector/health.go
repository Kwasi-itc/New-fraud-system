package sourcecollector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type OperationalAlert struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Count    int64  `json:"count"`
}
type OperationalHealth struct {
	Status             string             `json:"status"`
	Scope              string             `json:"scope"`
	MaxQueueAgeSeconds int64              `json:"max_queue_age_seconds"`
	Alerts             []OperationalAlert `json:"alerts"`
	Diagnostics        json.RawMessage    `json:"diagnostics"`
}

// Health is a private operator read and exit-status contract. It neither sends
// external notifications nor approves quality, rights or production activation.
func (c Collector) Health(ctx context.Context, maxQueueAge time.Duration) (OperationalHealth, error) {
	raw, e := c.Diagnostics(ctx)
	if e != nil {
		return OperationalHealth{}, e
	}
	return EvaluateOperationalHealth(raw, maxQueueAge)
}

func EvaluateOperationalHealth(raw json.RawMessage, maxQueueAge time.Duration) (OperationalHealth, error) {
	if maxQueueAge < 30*time.Second || maxQueueAge > 24*time.Hour {
		return OperationalHealth{}, fmt.Errorf("operational queue-age threshold must be 30s..24h")
	}
	var counters map[string]json.RawMessage
	if json.Unmarshal(raw, &counters) != nil {
		return OperationalHealth{}, fmt.Errorf("invalid operational diagnostics")
	}
	values := map[string]int64{}
	for _, name := range []string{"collection_enabled", "enabled_not_ready", "quarantined", "expired_source_leases", "rescreen_retry_exhausted", "source_retry_exhausted", "rescreen_failed", "oldest_publication_seconds", "oldest_rescreen_seconds"} {
		var n *int64
		if json.Unmarshal(counters[name], &n) != nil || n == nil || *n < 0 {
			return OperationalHealth{}, fmt.Errorf("incomplete operational diagnostics")
		}
		values[name] = *n
	}
	out := OperationalHealth{Status: "healthy", Scope: "Operational signals only; not source-use, matching-quality or production acceptance", MaxQueueAgeSeconds: int64(maxQueueAge.Seconds()), Alerts: []OperationalAlert{}, Diagnostics: raw}
	for _, name := range []string{"enabled_not_ready", "quarantined", "source_retry_exhausted", "rescreen_retry_exhausted"} {
		if values[name] > 0 {
			out.Alerts = append(out.Alerts, OperationalAlert{name, "critical", values[name]})
		}
	}
	for _, name := range []string{"expired_source_leases", "rescreen_failed"} {
		if values[name] > 0 {
			out.Alerts = append(out.Alerts, OperationalAlert{name, "warning", values[name]})
		}
	}
	for _, name := range []string{"oldest_publication_seconds", "oldest_rescreen_seconds"} {
		if values[name] > out.MaxQueueAgeSeconds {
			out.Alerts = append(out.Alerts, OperationalAlert{name, "critical", values[name]})
		}
	}
	if len(out.Alerts) > 0 {
		out.Status = "degraded"
	} else if values["collection_enabled"] == 0 {
		out.Status = "inactive"
	}
	return out, nil
}
