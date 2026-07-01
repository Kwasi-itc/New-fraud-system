package service

import (
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
)

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

func TestNormalizeRecurringScheduleConfigAllowsIanaTimezone(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:   true,
		Frequency: "daily",
		TimeOfDay: "08:00",
		Timezone:  "America/New_York",
	})
	if err != nil {
		t.Fatalf("normalize recurring schedule: %v", err)
	}
	if cfg.Timezone != "America/New_York" {
		t.Fatalf("expected timezone to be preserved, got %q", cfg.Timezone)
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

func TestNormalizeRecurringScheduleConfigHourlyDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 15,
	})
	if err != nil {
		t.Fatalf("normalize recurring schedule: %v", err)
	}
	if cfg.Frequency != "hourly" {
		t.Fatalf("expected hourly frequency, got %q", cfg.Frequency)
	}
	if cfg.MinuteOfHour != 15 {
		t.Fatalf("expected minute_of_hour 15, got %d", cfg.MinuteOfHour)
	}
	if cfg.TimeOfDay != "" {
		t.Fatalf("expected empty time_of_day for hourly schedule, got %q", cfg.TimeOfDay)
	}
}

func TestNormalizeRecurringScheduleConfigWeeklyDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:   true,
		Frequency: "weekly",
		TimeOfDay: "09:30",
		DayOfWeek: "wed",
	})
	if err != nil {
		t.Fatalf("normalize recurring schedule: %v", err)
	}
	if cfg.DayOfWeek != "wednesday" {
		t.Fatalf("expected normalized weekday, got %q", cfg.DayOfWeek)
	}
}

func TestNormalizeRecurringScheduleConfigRejectsInvalidHourlyMinute(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 60,
	})
	if err == nil {
		t.Fatal("expected invalid minute_of_hour error")
	}
}

func TestNextScheduledTimeHourly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 1, 10, 20, 0, 0, time.UTC)
	scheduledFor, err := nextScheduledTime(now, RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 15,
	})
	if err != nil {
		t.Fatalf("nextScheduledTime() error = %v", err)
	}

	want := time.Date(2026, time.July, 1, 10, 15, 0, 0, time.UTC)
	if !scheduledFor.Equal(want) {
		t.Fatalf("nextScheduledTime() = %v, want %v", scheduledFor, want)
	}
}

func TestNextScheduledTimeWeekly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC) // Thursday
	scheduledFor, err := nextScheduledTime(now, RecurringScheduleConfig{
		Enabled:   true,
		Frequency: "weekly",
		DayOfWeek: "monday",
		TimeOfDay: "08:30",
	})
	if err != nil {
		t.Fatalf("nextScheduledTime() error = %v", err)
	}

	want := time.Date(2026, time.June, 29, 8, 30, 0, 0, time.UTC)
	if !scheduledFor.Equal(want) {
		t.Fatalf("nextScheduledTime() = %v, want %v", scheduledFor, want)
	}
}

func TestNextFutureScheduledTimeUsesTimezone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 14, 0, 0, 0, time.UTC)
	nextRun, err := nextFutureScheduledTime(now, RecurringScheduleConfig{
		Enabled:   true,
		Frequency: "daily",
		TimeOfDay: "11:00",
		Timezone:  "America/New_York",
	})
	if err != nil {
		t.Fatalf("nextFutureScheduledTime() error = %v", err)
	}

	want := time.Date(2026, time.July, 2, 15, 0, 0, 0, time.UTC)
	if !nextRun.Equal(want) {
		t.Fatalf("nextFutureScheduledTime() = %v, want %v", nextRun, want)
	}
}

func TestNormalizeRecurringScheduleConfigMonthlyDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeRecurringScheduleConfig(RecurringScheduleConfig{
		Enabled:    true,
		Frequency:  "monthly",
		TimeOfDay:  "07:45",
		DayOfMonth: 31,
	})
	if err != nil {
		t.Fatalf("normalize recurring schedule: %v", err)
	}
	if cfg.DayOfMonth != 31 {
		t.Fatalf("expected day_of_month 31, got %d", cfg.DayOfMonth)
	}
}

func TestNextScheduledTimeMonthly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 20, 10, 0, 0, 0, time.UTC)
	scheduledFor, err := nextScheduledTime(now, RecurringScheduleConfig{
		Enabled:    true,
		Frequency:  "monthly",
		DayOfMonth: 15,
		TimeOfDay:  "09:30",
	})
	if err != nil {
		t.Fatalf("nextScheduledTime() error = %v", err)
	}

	want := time.Date(2026, time.February, 15, 9, 30, 0, 0, time.UTC)
	if !scheduledFor.Equal(want) {
		t.Fatalf("nextScheduledTime() = %v, want %v", scheduledFor, want)
	}
}

func TestNextFutureScheduledTimeMonthlyClampsDay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 29, 10, 0, 0, 0, time.UTC)
	nextRun, err := nextFutureScheduledTime(now, RecurringScheduleConfig{
		Enabled:    true,
		Frequency:  "monthly",
		DayOfMonth: 31,
		TimeOfDay:  "09:30",
	})
	if err != nil {
		t.Fatalf("nextFutureScheduledTime() error = %v", err)
	}

	want := time.Date(2026, time.April, 30, 9, 30, 0, 0, time.UTC)
	if !nextRun.Equal(want) {
		t.Fatalf("nextFutureScheduledTime() = %v, want %v", nextRun, want)
	}
}

func TestHydrateRecurringScheduleSetsNextRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 1, 10, 20, 0, 0, time.UTC)
	cfg, err := hydrateRecurringSchedule(RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 45,
	}, now, nil)
	if err != nil {
		t.Fatalf("hydrateRecurringSchedule() error = %v", err)
	}
	if cfg.NextRun == nil {
		t.Fatal("expected next_run to be set")
	}
	want := time.Date(2026, time.July, 1, 10, 45, 0, 0, time.UTC)
	if !cfg.NextRun.Equal(want) {
		t.Fatalf("hydrateRecurringSchedule() next_run = %v, want %v", cfg.NextRun, want)
	}
}

func TestHydrateRecurringScheduleSetsLastRunFromNonPendingExecution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 1, 10, 20, 0, 0, time.UTC)
	lastScheduledFor := time.Date(2026, time.July, 1, 9, 45, 0, 0, time.UTC)
	cfg, err := hydrateRecurringSchedule(RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 45,
	}, now, []execution.ScheduledExecution{
		{
			Source:       execution.SourceRecurring,
			Status:       execution.StatusCompleted,
			ScheduledFor: lastScheduledFor,
		},
	})
	if err != nil {
		t.Fatalf("hydrateRecurringSchedule() error = %v", err)
	}
	if cfg.LastRun == nil || !cfg.LastRun.Equal(lastScheduledFor) {
		t.Fatalf("hydrateRecurringSchedule() last_run = %v, want %v", cfg.LastRun, lastScheduledFor)
	}
}

func TestDueRecurringScheduleTimesHourlyBoundedCatchup(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 1, 10, 20, 0, 0, time.UTC)
	due, err := dueRecurringScheduleTimes(now, RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 15,
	}, nil, 3)
	if err != nil {
		t.Fatalf("dueRecurringScheduleTimes() error = %v", err)
	}
	if len(due) != 3 {
		t.Fatalf("dueRecurringScheduleTimes() len = %d, want 3", len(due))
	}
	want := []time.Time{
		time.Date(2026, time.July, 1, 8, 15, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 9, 15, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 10, 15, 0, 0, time.UTC),
	}
	for index := range want {
		if !due[index].Equal(want[index]) {
			t.Fatalf("due[%d] = %v, want %v", index, due[index], want[index])
		}
	}
}

func TestDueRecurringScheduleTimesSkipsExistingRecurringOccurrences(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 1, 10, 20, 0, 0, time.UTC)
	due, err := dueRecurringScheduleTimes(now, RecurringScheduleConfig{
		Enabled:      true,
		Frequency:    "hourly",
		MinuteOfHour: 15,
	}, []execution.ScheduledExecution{
		{
			Source:       execution.SourceRecurring,
			Status:       execution.StatusCompleted,
			ScheduledFor: time.Date(2026, time.July, 1, 10, 15, 0, 0, time.UTC),
		},
	}, 2)
	if err != nil {
		t.Fatalf("dueRecurringScheduleTimes() error = %v", err)
	}
	want := []time.Time{
		time.Date(2026, time.July, 1, 8, 15, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 9, 15, 0, 0, time.UTC),
	}
	if len(due) != len(want) {
		t.Fatalf("dueRecurringScheduleTimes() len = %d, want %d", len(due), len(want))
	}
	for index := range want {
		if !due[index].Equal(want[index]) {
			t.Fatalf("due[%d] = %v, want %v", index, due[index], want[index])
		}
	}
}
