package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	defaultRecurringTimezone      = "UTC"
	defaultRecurringCandidateLimit = 100
)

type RecurringScheduleConfig struct {
	Enabled        bool   `json:"enabled"`
	Frequency      string `json:"frequency"`
	TimeOfDay      string `json:"time_of_day"`
	Timezone       string `json:"timezone"`
	CandidateLimit int    `json:"candidate_limit"`
}

func NormalizeRecurringScheduleConfig(cfg RecurringScheduleConfig) (RecurringScheduleConfig, error) {
	if !cfg.Enabled {
		return RecurringScheduleConfig{}, nil
	}

	cfg.Frequency = strings.TrimSpace(strings.ToLower(cfg.Frequency))
	if cfg.Frequency == "" {
		cfg.Frequency = "daily"
	}
	if cfg.Frequency != "daily" {
		return RecurringScheduleConfig{}, fmt.Errorf("frequency must be daily")
	}

	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	if cfg.Timezone == "" {
		cfg.Timezone = defaultRecurringTimezone
	}
	if cfg.Timezone != defaultRecurringTimezone {
		return RecurringScheduleConfig{}, fmt.Errorf("timezone must be UTC")
	}

	cfg.TimeOfDay = strings.TrimSpace(cfg.TimeOfDay)
	if _, err := time.Parse("15:04", cfg.TimeOfDay); err != nil {
		return RecurringScheduleConfig{}, fmt.Errorf("time_of_day must use HH:MM in 24-hour format")
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

func nextScheduledTimeForDay(now time.Time, cfg RecurringScheduleConfig) (time.Time, error) {
	parsed, err := time.Parse("15:04", cfg.TimeOfDay)
	if err != nil {
		return time.Time{}, err
	}

	return time.Date(
		now.UTC().Year(),
		now.UTC().Month(),
		now.UTC().Day(),
		parsed.Hour(),
		parsed.Minute(),
		0,
		0,
		time.UTC,
	), nil
}
