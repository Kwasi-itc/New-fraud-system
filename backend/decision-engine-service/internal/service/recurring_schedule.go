package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
)

const (
	defaultRecurringTimezone      = "UTC"
	defaultRecurringCandidateLimit = 100
	maxRecurringCatchupOccurrences = 24
)

type RecurringScheduleConfig struct {
	Enabled        bool   `json:"enabled"`
	Frequency      string `json:"frequency"`
	TimeOfDay      string `json:"time_of_day"`
	MinuteOfHour   int    `json:"minute_of_hour,omitempty"`
	DayOfWeek      string `json:"day_of_week,omitempty"`
	DayOfMonth     int    `json:"day_of_month,omitempty"`
	Timezone       string `json:"timezone"`
	CandidateLimit int    `json:"candidate_limit"`
	NextRun        *time.Time `json:"-"`
	LastRun        *time.Time `json:"-"`
}

func NormalizeRecurringScheduleConfig(cfg RecurringScheduleConfig) (RecurringScheduleConfig, error) {
	if !cfg.Enabled {
		return RecurringScheduleConfig{}, nil
	}

	cfg.Frequency = strings.TrimSpace(strings.ToLower(cfg.Frequency))
	if cfg.Frequency == "" {
		cfg.Frequency = "daily"
	}
	if cfg.Frequency != "daily" && cfg.Frequency != "hourly" && cfg.Frequency != "weekly" && cfg.Frequency != "monthly" {
		return RecurringScheduleConfig{}, fmt.Errorf("frequency must be one of daily, hourly, weekly, monthly")
	}

	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	if cfg.Timezone == "" {
		cfg.Timezone = defaultRecurringTimezone
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return RecurringScheduleConfig{}, fmt.Errorf("timezone must be a valid IANA timezone")
	}

	switch cfg.Frequency {
	case "hourly":
		if cfg.MinuteOfHour < 0 || cfg.MinuteOfHour > 59 {
			return RecurringScheduleConfig{}, fmt.Errorf("minute_of_hour must be between 0 and 59")
		}
		cfg.TimeOfDay = ""
		cfg.DayOfWeek = ""
		cfg.DayOfMonth = 0
	case "daily":
		cfg.TimeOfDay = strings.TrimSpace(cfg.TimeOfDay)
		if _, err := time.Parse("15:04", cfg.TimeOfDay); err != nil {
			return RecurringScheduleConfig{}, fmt.Errorf("time_of_day must use HH:MM in 24-hour format")
		}
		cfg.MinuteOfHour = 0
		cfg.DayOfWeek = ""
		cfg.DayOfMonth = 0
	case "weekly":
		cfg.TimeOfDay = strings.TrimSpace(cfg.TimeOfDay)
		if _, err := time.Parse("15:04", cfg.TimeOfDay); err != nil {
			return RecurringScheduleConfig{}, fmt.Errorf("time_of_day must use HH:MM in 24-hour format")
		}
		cfg.DayOfWeek = normalizeWeekday(cfg.DayOfWeek)
		if cfg.DayOfWeek == "" {
			return RecurringScheduleConfig{}, fmt.Errorf("day_of_week must be one of monday, tuesday, wednesday, thursday, friday, saturday, sunday")
		}
		cfg.MinuteOfHour = 0
		cfg.DayOfMonth = 0
	case "monthly":
		cfg.TimeOfDay = strings.TrimSpace(cfg.TimeOfDay)
		if _, err := time.Parse("15:04", cfg.TimeOfDay); err != nil {
			return RecurringScheduleConfig{}, fmt.Errorf("time_of_day must use HH:MM in 24-hour format")
		}
		if cfg.DayOfMonth < 1 || cfg.DayOfMonth > 31 {
			return RecurringScheduleConfig{}, fmt.Errorf("day_of_month must be between 1 and 31")
		}
		cfg.MinuteOfHour = 0
		cfg.DayOfWeek = ""
	}

	if cfg.CandidateLimit <= 0 {
		cfg.CandidateLimit = defaultRecurringCandidateLimit
	}

	return cfg, nil
}

func EncodeRecurringScheduleConfig(cfg RecurringScheduleConfig) (string, error) {
	normalized, err := NormalizeRecurringScheduleConfig(cfg)
	if err != nil {
		return "", err
	}
	if !normalized.Enabled {
		return "", nil
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func DecodeRecurringScheduleConfig(raw string) (RecurringScheduleConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RecurringScheduleConfig{}, nil
	}

	var cfg RecurringScheduleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return RecurringScheduleConfig{}, fmt.Errorf("decode recurring schedule: %w", err)
	}
	return NormalizeRecurringScheduleConfig(cfg)
}

func normalizeWeekday(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "mon", "monday":
		return "monday"
	case "tue", "tues", "tuesday":
		return "tuesday"
	case "wed", "wednesday":
		return "wednesday"
	case "thu", "thurs", "thursday":
		return "thursday"
	case "fri", "friday":
		return "friday"
	case "sat", "saturday":
		return "saturday"
	case "sun", "sunday":
		return "sunday"
	default:
		return ""
	}
}

func weekdayIndex(value string) (time.Weekday, error) {
	switch normalizeWeekday(value) {
	case "monday":
		return time.Monday, nil
	case "tuesday":
		return time.Tuesday, nil
	case "wednesday":
		return time.Wednesday, nil
	case "thursday":
		return time.Thursday, nil
	case "friday":
		return time.Friday, nil
	case "saturday":
		return time.Saturday, nil
	case "sunday":
		return time.Sunday, nil
	default:
		return time.Sunday, fmt.Errorf("invalid day_of_week")
	}
}

func nextScheduledTime(now time.Time, cfg RecurringScheduleConfig) (time.Time, error) {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	nowLocal := now.In(location)

	switch cfg.Frequency {
	case "hourly":
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			nowLocal.Day(),
			nowLocal.Hour(),
			cfg.MinuteOfHour,
			0,
			0,
			location,
		)
		if candidate.After(nowLocal) {
			candidate = candidate.Add(-time.Hour)
		}
		return candidate.UTC(), nil
	case "weekly":
		weekday, err := weekdayIndex(cfg.DayOfWeek)
		if err != nil {
			return time.Time{}, err
		}
		parsed, err := time.Parse("15:04", cfg.TimeOfDay)
		if err != nil {
			return time.Time{}, err
		}
		daysBack := (int(now.Weekday()) - int(weekday) + 7) % 7
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			nowLocal.Day()-daysBack,
			parsed.Hour(),
			parsed.Minute(),
			0,
			0,
			location,
		)
		if candidate.After(nowLocal) {
			candidate = candidate.AddDate(0, 0, -7)
		}
		return candidate.UTC(), nil
	case "monthly":
		parsed, err := time.Parse("15:04", cfg.TimeOfDay)
		if err != nil {
			return time.Time{}, err
		}
		day := minInt(cfg.DayOfMonth, daysInMonth(nowLocal.Year(), nowLocal.Month(), location))
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			day,
			parsed.Hour(),
			parsed.Minute(),
			0,
			0,
			location,
		)
		if candidate.After(nowLocal) {
			prevMonth := nowLocal.AddDate(0, -1, 0)
			day = minInt(cfg.DayOfMonth, daysInMonth(prevMonth.Year(), prevMonth.Month(), location))
			candidate = time.Date(
				prevMonth.Year(),
				prevMonth.Month(),
				day,
				parsed.Hour(),
				parsed.Minute(),
				0,
				0,
				location,
			)
		}
		return candidate.UTC(), nil
	default:
		return nextScheduledTimeForDay(now, cfg)
	}
}

func previousScheduledTime(current time.Time, cfg RecurringScheduleConfig) (time.Time, error) {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	currentLocal := current.In(location)

	switch cfg.Frequency {
	case "hourly":
		return currentLocal.Add(-time.Hour).UTC(), nil
	case "weekly":
		return currentLocal.AddDate(0, 0, -7).UTC(), nil
	case "monthly":
		prevMonth := currentLocal.AddDate(0, -1, 0)
		day := minInt(cfg.DayOfMonth, daysInMonth(prevMonth.Year(), prevMonth.Month(), location))
		return time.Date(
			prevMonth.Year(),
			prevMonth.Month(),
			day,
			currentLocal.Hour(),
			currentLocal.Minute(),
			0,
			0,
			location,
		).UTC(), nil
	default:
		return currentLocal.AddDate(0, 0, -1).UTC(), nil
	}
}

func nextFutureScheduledTime(now time.Time, cfg RecurringScheduleConfig) (time.Time, error) {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	nowLocal := now.In(location)

	switch cfg.Frequency {
	case "hourly":
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			nowLocal.Day(),
			nowLocal.Hour(),
			cfg.MinuteOfHour,
			0,
			0,
			location,
		)
		if candidate.Before(nowLocal) {
			candidate = candidate.Add(time.Hour)
		}
		return candidate.UTC(), nil
	case "weekly":
		weekday, err := weekdayIndex(cfg.DayOfWeek)
		if err != nil {
			return time.Time{}, err
		}
		parsed, err := time.Parse("15:04", cfg.TimeOfDay)
		if err != nil {
			return time.Time{}, err
		}
		daysForward := (int(weekday) - int(now.Weekday()) + 7) % 7
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			nowLocal.Day()+daysForward,
			parsed.Hour(),
			parsed.Minute(),
			0,
			0,
			location,
		)
		if candidate.Before(nowLocal) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate.UTC(), nil
	case "monthly":
		parsed, err := time.Parse("15:04", cfg.TimeOfDay)
		if err != nil {
			return time.Time{}, err
		}
		day := minInt(cfg.DayOfMonth, daysInMonth(nowLocal.Year(), nowLocal.Month(), location))
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			day,
			parsed.Hour(),
			parsed.Minute(),
			0,
			0,
			location,
		)
		if candidate.Before(nowLocal) {
			nextMonth := nowLocal.AddDate(0, 1, 0)
			day = minInt(cfg.DayOfMonth, daysInMonth(nextMonth.Year(), nextMonth.Month(), location))
			candidate = time.Date(
				nextMonth.Year(),
				nextMonth.Month(),
				day,
				parsed.Hour(),
				parsed.Minute(),
				0,
				0,
				location,
			)
		}
		return candidate.UTC(), nil
	default:
		parsed, err := time.Parse("15:04", cfg.TimeOfDay)
		if err != nil {
			return time.Time{}, err
		}
		candidate := time.Date(
			nowLocal.Year(),
			nowLocal.Month(),
			nowLocal.Day(),
			parsed.Hour(),
			parsed.Minute(),
			0,
			0,
			location,
		)
		if candidate.Before(nowLocal) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate.UTC(), nil
	}
}

func hydrateRecurringSchedule(
	cfg RecurringScheduleConfig,
	now time.Time,
	items []execution.ScheduledExecution,
) (RecurringScheduleConfig, error) {
	if !cfg.Enabled {
		return cfg, nil
	}

	nextRun, err := nextFutureScheduledTime(now, cfg)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	cfg.NextRun = &nextRun
	cfg.LastRun = deriveLastRun(items)
	return cfg, nil
}

func dueRecurringScheduleTimes(
	now time.Time,
	cfg RecurringScheduleConfig,
	items []execution.ScheduledExecution,
	limit int,
) ([]time.Time, error) {
	latestDue, err := nextScheduledTime(now, cfg)
	if err != nil {
		return nil, err
	}

	maxOccurrences := minInt(maxRecurringCatchupOccurrences, maxInt(1, limit))
	existing := make(map[time.Time]struct{}, len(items))
	for _, item := range items {
		if item.Source != execution.SourceRecurring {
			continue
		}
		existing[item.ScheduledFor.UTC()] = struct{}{}
	}

	due := make([]time.Time, 0, maxOccurrences)
	current := latestDue
	for len(due) < maxOccurrences {
		if _, ok := existing[current.UTC()]; !ok {
			due = append(due, current.UTC())
		}
		prev, err := previousScheduledTime(current, cfg)
		if err != nil {
			return nil, err
		}
		if !prev.Before(current) {
			break
		}
		current = prev
		if current.After(now.UTC()) {
			break
		}
	}

	for left, right := 0, len(due)-1; left < right; left, right = left+1, right-1 {
		due[left], due[right] = due[right], due[left]
	}

	return due, nil
}

func deriveLastRun(items []execution.ScheduledExecution) *time.Time {
	var lastRun *time.Time
	for _, item := range items {
		if item.Status == execution.StatusPending || item.Status == execution.StatusQueued {
			continue
		}
		scheduledFor := item.ScheduledFor
		if lastRun == nil || scheduledFor.After(*lastRun) {
			copied := scheduledFor
			lastRun = &copied
		}
	}
	return lastRun
}

func daysInMonth(year int, month time.Month, location *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func nextScheduledTimeForDay(now time.Time, cfg RecurringScheduleConfig) (time.Time, error) {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse("15:04", cfg.TimeOfDay)
	if err != nil {
		return time.Time{}, err
	}
	nowLocal := now.In(location)

	candidate := time.Date(
		nowLocal.Year(),
		nowLocal.Month(),
		nowLocal.Day(),
		parsed.Hour(),
		parsed.Minute(),
		0,
		0,
		location,
	)
	if candidate.After(nowLocal) {
		candidate = candidate.AddDate(0, 0, -1)
	}
	return candidate.UTC(), nil
}
