package casepkg

import (
	"github.com/google/uuid"
	"time"
)

type AnalyticsFilter struct {
	From, To     time.Time
	InboxID      *uuid.UUID
	AccessUserID string
}
type AnalyticsCounts struct {
	Total               int64    `json:"total"`
	Pending             int64    `json:"pending"`
	Investigating       int64    `json:"investigating"`
	Closed              int64    `json:"closed"`
	FalsePositive       int64    `json:"false_positive"`
	ValuableAlert       int64    `json:"valuable_alert"`
	ConfirmedRisk       int64    `json:"confirmed_risk"`
	Unset               int64    `json:"unset"`
	Snoozed             int64    `json:"snoozed"`
	SLAConfigured       int64    `json:"sla_configured"`
	Overdue             int64    `json:"overdue"`
	Escalations         int64    `json:"escalations"`
	SnoozeEvents        int64    `json:"snooze_events"`
	MeasuredClosures    int64    `json:"measured_closures"`
	AverageCloseSeconds *float64 `json:"average_close_seconds"`
}
type InboxAnalytics struct {
	InboxID   uuid.UUID `json:"inbox_id"`
	InboxName string    `json:"inbox_name"`
	AnalyticsCounts
}
type DailyAnalytics struct {
	Date          string `json:"date"`
	Total         int64  `json:"total"`
	Pending       int64  `json:"pending"`
	Investigating int64  `json:"investigating"`
	Closed        int64  `json:"closed"`
}
type AssignmentAnalytics struct {
	InboxID  uuid.UUID `json:"inbox_id"`
	Assignee *string   `json:"assignee"`
	Open     int64     `json:"open"`
}
type Analytics struct {
	AsOf        time.Time             `json:"as_of"`
	From        time.Time             `json:"from"`
	To          time.Time             `json:"to"`
	Totals      AnalyticsCounts       `json:"totals"`
	Inboxes     []InboxAnalytics      `json:"inboxes"`
	Daily       []DailyAnalytics      `json:"daily"`
	Assignments []AssignmentAnalytics `json:"assignments"`
}
