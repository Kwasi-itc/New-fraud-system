package service

import "testing"

func TestNormalizeRecurringScheduleConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:   true,
		Frequency: "daily",
		TimeOfDay: "00:00",
	})
	if err != nil {
		t.Fatalf("normalize recurring schedule: %v", err)
	}
	if cfg.Timezone != "UTC" {
		t.Fatalf("expected UTC timezone, got %q", cfg.Timezone)
	}
	if cfg.CandidateLimit != 100 {
		t.Fatalf("expected default candidate limit 100, got %d", cfg.CandidateLimit)
	}
}

func TestEncodeDecodeRecurringScheduleConfigRoundTrip(t *testing.T) {
	t.Parallel()

	raw, err := EncodeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:        true,
		Frequency:      "daily",
		TimeOfDay:      "06:30",
		Timezone:       "UTC",
		CandidateLimit: 250,
	})
	if err != nil {
		t.Fatalf("encode recurring schedule: %v", err)
	}

	cfg, err := DecodeRecurringScheduleConfig(raw)
	if err != nil {
		t.Fatalf("decode recurring schedule: %v", err)
	}
	if !cfg.Enabled || cfg.Frequency != "daily" || cfg.TimeOfDay != "06:30" || cfg.CandidateLimit != 250 {
		t.Fatalf("unexpected decoded config: %+v", cfg)
	}
}
