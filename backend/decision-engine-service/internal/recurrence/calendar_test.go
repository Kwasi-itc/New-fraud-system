package recurrence

import (
	"testing"
	"time"
)

func daily(zone, clock string) Spec {
	return Spec{Version: Version, Frequency: "daily", Timezone: zone, TimeOfDay: clock, GapPolicy: SkipGap, OverlapPolicy: FirstOverlap}
}
func calendar(t *testing.T, spec Spec) *Calendar {
	t.Helper()
	c, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func slot(t *testing.T, c *Calendar, text string) Slot {
	t.Helper()
	s, err := c.ParseSlot(text)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCalendarMonthClampNeverSkipsOrDriftsMonth(t *testing.T) {
	spec := daily("UTC", "09:30")
	spec.Frequency, spec.DayOfMonth = "monthly", 31
	c := calendar(t, spec)
	for _, corpus := range [][]string{{"2026-01-31T09:30", "2026-02-28T09:30", "2026-03-31T09:30", "2026-04-30T09:30"}, {"2028-01-31T09:30", "2028-02-29T09:30", "2028-03-31T09:30"}} {
		for i := 0; i < len(corpus)-1; i++ {
			next, err := c.Next(slot(t, c, corpus[i]))
			if err != nil || next.String() != corpus[i+1] {
				t.Fatal(next, err)
			}
			previous, err := c.Previous(next)
			if err != nil || previous.String() != corpus[i] {
				t.Fatal(previous, err)
			}
		}
	}
	if _, err := c.ParseSlot("2026-02-27T09:30"); err == nil {
		t.Fatal("non-clamped monthly date accepted")
	}
}

func TestCalendarDSTGapSkippedAndOverlapRunsOnceAtEarlierInstant(t *testing.T) {
	c := calendar(t, daily("America/New_York", "02:30"))
	if _, exists, err := c.Resolve(slot(t, c, "2026-03-08T02:30")); err != nil || exists {
		t.Fatal("gap was normalized into a real occurrence", exists, err)
	}
	next, err := c.Next(slot(t, c, "2026-03-08T02:30"))
	if err != nil {
		t.Fatal(err)
	}
	instant, exists, err := c.Resolve(next)
	if err != nil || !exists || instant.Format(time.RFC3339) != "2026-03-09T06:30:00Z" {
		t.Fatal(instant, exists, err)
	}
	c = calendar(t, daily("America/New_York", "01:30"))
	instant, exists, err = c.Resolve(slot(t, c, "2026-11-01T01:30"))
	if err != nil || !exists || instant.Format(time.RFC3339) != "2026-11-01T05:30:00Z" {
		t.Fatal("overlap did not choose earlier instant", instant, err)
	}
	if _, exists, err := calendar(t, daily("Australia/Lord_Howe", "02:15")).Resolve(slot(t, calendar(t, daily("Australia/Lord_Howe", "02:15")), "2026-10-04T02:15")); err != nil || exists {
		t.Fatal("half-hour gap", exists, err)
	}
	apia := calendar(t, daily("Pacific/Apia", "09:00"))
	if _, exists, err := apia.Resolve(slot(t, apia, "2011-12-30T09:00")); err != nil || exists {
		t.Fatal("whole skipped civil day", exists, err)
	}
}

func TestCalendarHourlyOverlapAndLocalWeekday(t *testing.T) {
	spec := daily("America/New_York", "")
	spec.Frequency, spec.MinuteOfHour = "hourly", 30
	c := calendar(t, spec)
	s := slot(t, c, "2026-11-01T01:30")
	first, _, err := c.Resolve(s)
	if err != nil {
		t.Fatal(err)
	}
	next, err := c.Next(s)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := c.Resolve(next)
	if err != nil || second.Sub(first) != 2*time.Hour || next.String() != "2026-11-01T02:30" {
		t.Fatal("overlap repeated a logical hour", first, second, err)
	}
	spec = daily("America/Los_Angeles", "23:30")
	spec.Frequency, spec.DayOfWeek = "weekly", "sunday"
	c = calendar(t, spec)
	instant, exists, err := c.Resolve(slot(t, c, "2026-10-25T23:30"))
	if err != nil || !exists || instant.Weekday() != time.Monday || instant.Format(time.RFC3339) != "2026-10-26T06:30:00Z" {
		t.Fatal("used UTC weekday instead of local weekday", instant, err)
	}
}

func TestCalendarExplicitValidationAndWideCivilRange(t *testing.T) {
	for _, change := range []func(*Spec){func(s *Spec) { s.Version = 0 }, func(s *Spec) { s.Timezone = "" }, func(s *Spec) { s.Timezone = "Local" }, func(s *Spec) { s.Frequency = "" }, func(s *Spec) { s.TimeOfDay = "9:30" }, func(s *Spec) { s.DayOfWeek = "monday" }, func(s *Spec) { s.GapPolicy = "" }, func(s *Spec) { s.OverlapPolicy = "both" }, func(s *Spec) { s.Frequency = "monthly"; s.DayOfMonth = 32 }} {
		spec := daily("UTC", "09:30")
		change(&spec)
		if _, err := New(spec); err == nil {
			t.Fatal("invalid spec accepted", spec)
		}
	}
	c := calendar(t, daily("UTC", "00:00"))
	first, last := slot(t, c, "1000-01-01T00:00"), slot(t, c, "9999-12-31T00:00")
	distance, err := c.Distance(first, last)
	if err != nil || distance < 3000000 {
		t.Fatal("civil distance overflowed or iterated elapsed durations", distance, err)
	}
	if _, err := c.Next(last); err == nil {
		t.Fatal("out-of-range next accepted")
	}
	if _, err := c.Previous(first); err == nil {
		t.Fatal("out-of-range previous accepted")
	}
	if _, exists, err := c.Resolve(Slot{}); err == nil || exists {
		t.Fatal("invalid slot became a successful calendar skip")
	}
}
