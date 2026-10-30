// Package recurrence defines the corrected, staged decision-owner calendar
// contract. It neither changes the PostgreSQL scheduler nor activates workers.
package recurrence

import (
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // Fallback for minimal images; deployments must pin tzdb too.
)

var ErrCalendar = errors.New("invalid or unsupported calendar contract/slot")

const (
	Version      = 1
	SkipGap      = "skip"
	FirstOverlap = "first"
)

// Spec is canonical and explicit, not a permissive HTTP/source decoder. There
// are no guessed frequency/timezone/DST defaults or ignored unrelated fields.
// A source adapter must normalize its public DTO before constructing this spec.
// Deployments must use the same timezone database release across replicas.
type Spec struct {
	Version       int    `json:"version"`
	Frequency     string `json:"frequency"`
	Timezone      string `json:"timezone"`
	TimeOfDay     string `json:"time_of_day,omitempty"`
	MinuteOfHour  int    `json:"minute_of_hour,omitempty"`
	DayOfWeek     string `json:"day_of_week,omitempty"`
	DayOfMonth    int    `json:"day_of_month,omitempty"`
	GapPolicy     string `json:"gap_policy"`
	OverlapPolicy string `json:"overlap_policy"`
}

type Calendar struct {
	spec         Spec
	location     *time.Location
	hour, minute int
	weekday      time.Weekday
}

func New(spec Spec) (*Calendar, error) {
	if spec.Version != Version || spec.Timezone == "" || spec.Timezone == "Local" || spec.GapPolicy != SkipGap || spec.OverlapPolicy != FirstOverlap {
		return nil, ErrCalendar
	}
	location, err := time.LoadLocation(spec.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: timezone: %v", ErrCalendar, err)
	}
	c := &Calendar{spec: spec, location: location}
	if spec.Frequency == "hourly" {
		if spec.MinuteOfHour < 0 || spec.MinuteOfHour > 59 || spec.TimeOfDay != "" || spec.DayOfWeek != "" || spec.DayOfMonth != 0 {
			return nil, ErrCalendar
		}
		c.minute = spec.MinuteOfHour
		return c, nil
	}
	clock, err := time.Parse("15:04", spec.TimeOfDay)
	if err != nil || clock.Format("15:04") != spec.TimeOfDay || spec.MinuteOfHour != 0 {
		return nil, ErrCalendar
	}
	c.hour, c.minute = clock.Hour(), clock.Minute()
	switch spec.Frequency {
	case "daily":
		if spec.DayOfWeek != "" || spec.DayOfMonth != 0 {
			return nil, ErrCalendar
		}
	case "weekly":
		if spec.DayOfMonth != 0 {
			return nil, ErrCalendar
		}
		found := false
		for i, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
			if spec.DayOfWeek == name {
				c.weekday, found = time.Weekday(i), true
			}
		}
		if !found {
			return nil, ErrCalendar
		}
	case "monthly":
		if spec.DayOfWeek != "" || spec.DayOfMonth < 1 || spec.DayOfMonth > 31 {
			return nil, ErrCalendar
		}
	default:
		return nil, ErrCalendar
	}
	return c, nil
}

func (c *Calendar) Spec() Spec { return c.spec }

// Slot is a LOGICAL civil label, not an instant. Its internal UTC location is
// only a timezone-free representation for date arithmetic/storage. Persist the
// nominal cursor separately from next_at: a skipped DST slot has no instant.
type Slot struct{ label time.Time }

func (s Slot) String() string { return s.label.Format("2006-01-02T15:04") }

func (c *Calendar) ParseSlot(local string) (Slot, error) {
	label, err := time.Parse("2006-01-02T15:04", local)
	s := Slot{label}
	if err != nil || s.String() != local || !c.valid(s) {
		return Slot{}, ErrCalendar
	}
	return s, nil
}

func validLabel(t time.Time) bool {
	return t.Year() >= 1000 && t.Year() <= 9999 && t.Second() == 0 && t.Nanosecond() == 0 && t.Location() == time.UTC
}
func monthEnd(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
func (c *Calendar) valid(slot Slot) bool {
	if c == nil || c.location == nil || !validLabel(slot.label) || slot.label.Minute() != c.minute {
		return false
	}
	if c.spec.Frequency == "hourly" {
		return true
	}
	if slot.label.Hour() != c.hour {
		return false
	}
	if c.spec.Frequency == "weekly" {
		return slot.label.Weekday() == c.weekday
	}
	if c.spec.Frequency == "monthly" {
		return slot.label.Day() == min(c.spec.DayOfMonth, monthEnd(slot.label.Year(), slot.label.Month()))
	}
	return c.spec.Frequency == "daily"
}

func (c *Calendar) Next(slot Slot) (Slot, error)     { return c.advance(slot, 1) }
func (c *Calendar) Previous(slot Slot) (Slot, error) { return c.advance(slot, -1) }
func (c *Calendar) advance(slot Slot, direction int) (Slot, error) {
	if !c.valid(slot) {
		return Slot{}, ErrCalendar
	}
	t := slot.label
	switch c.spec.Frequency {
	case "hourly":
		t = t.Add(time.Duration(direction) * time.Hour)
	case "daily":
		t = t.AddDate(0, 0, direction)
	case "weekly":
		t = t.AddDate(0, 0, 7*direction)
	case "monthly":
		// Advance from the FIRST day, never Jan 31 + one month (which can
		// normalize into March). Clamp independently for the new month.
		month := time.Date(t.Year(), t.Month()+time.Month(direction), 1, c.hour, c.minute, 0, 0, time.UTC)
		t = time.Date(month.Year(), month.Month(), min(c.spec.DayOfMonth, monthEnd(month.Year(), month.Month())), c.hour, c.minute, 0, 0, time.UTC)
	}
	next := Slot{t}
	if !c.valid(next) {
		return Slot{}, ErrCalendar
	}
	return next, nil
}

// Distance counts nominal slots using integer civil arithmetic, not time.Sub
// (which overflows beyond ~292 years) or a loop over execution history. Gaps
// remain nominal slots and must be included in the owner's skip accounting.
func (c *Calendar) Distance(from, to Slot) (int64, error) {
	if !c.valid(from) || !c.valid(to) || to.label.Before(from.label) {
		return 0, ErrCalendar
	}
	switch c.spec.Frequency {
	case "hourly":
		return (to.label.Unix() - from.label.Unix()) / 3600, nil
	case "monthly":
		return int64(to.label.Year()-from.label.Year())*12 + int64(to.label.Month()-from.label.Month()), nil
	default:
		a := time.Date(from.label.Year(), from.label.Month(), from.label.Day(), 0, 0, 0, 0, time.UTC).Unix()
		b := time.Date(to.label.Year(), to.label.Month(), to.label.Day(), 0, 0, 0, 0, time.UTC).Unix()
		days := (b - a) / 86400
		if c.spec.Frequency == "weekly" {
			days /= 7
		}
		return days, nil
	}
}

// Resolve explicitly handles civil ambiguity instead of relying on time.Date's
// unspecified choice of zone in an overlap/gap. A nonexistent local slot is a
// policy skip (false), never an invalid-input no-match: invalid slots return an
// error. An overlap resolves to its EARLIEST instant, once per logical slot.
func (c *Calendar) Resolve(slot Slot) (time.Time, bool, error) {
	if !c.valid(slot) {
		return time.Time{}, false, ErrCalendar
	}
	wall := slot.label
	start, end := wall.Add(-48*time.Hour), wall.Add(48*time.Hour)
	var first time.Time
	for transitions := 0; ; transitions++ {
		if transitions == 16 {
			return time.Time{}, false, fmt.Errorf("%w: excessive timezone transitions", ErrCalendar)
		}
		_, offset := start.In(c.location).Zone()
		if offset <= -86400 || offset >= 86400 {
			return time.Time{}, false, fmt.Errorf("%w: unsupported timezone offset", ErrCalendar)
		}
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(c.location)
		if local.Year() == wall.Year() && local.Month() == wall.Month() && local.Day() == wall.Day() && local.Hour() == wall.Hour() && local.Minute() == wall.Minute() && local.Second() == 0 {
			if candidate.Year() < 1000 || candidate.Year() > 9999 {
				return time.Time{}, false, ErrCalendar
			}
			if first.IsZero() || candidate.Before(first) {
				first = candidate
			}
		}
		_, boundary := start.In(c.location).ZoneBounds()
		if boundary.IsZero() || !boundary.Before(end) {
			break
		}
		if !boundary.After(start) {
			return time.Time{}, false, ErrCalendar
		}
		start = boundary
	}
	return first, !first.IsZero(), nil
}
