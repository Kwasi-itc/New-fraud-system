package recurrence

import (
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

func instant(t *testing.T, text string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCalendarPlanGapCatchUpAndOverlapCoalesce(t *testing.T) {
	c := calendar(t, daily("America/New_York", "02:30"))
	next := slot(t, c, "2026-03-07T02:30")
	plan, err := c.PlanOccurrences(next, instant(t, "2026-03-10T07:00:00Z"), workers.CatchUp, 0, 1)
	if err != nil || plan.Skipped != 1 || !plan.MoreDue || plan.NextLocal != "2026-03-09T02:30" || len(plan.Occurrences) != 1 {
		t.Fatal(plan, err)
	}
	plan2, err := c.PlanOccurrences(slot(t, c, plan.NextLocal), instant(t, "2026-03-10T07:00:00Z"), workers.CatchUp, 0, 5)
	if err != nil || plan2.MoreDue || len(plan2.Occurrences) != 2 || plan2.Skipped != 0 || plan2.NextLocal != "2026-03-11T02:30" {
		t.Fatal(plan2, err)
	}
	spec := daily("America/New_York", "")
	spec.Frequency, spec.MinuteOfHour = "hourly", 30
	c = calendar(t, spec)
	// 06:15Z is the SECOND 01:15. First 01:30 (05:30Z) already happened.
	plan, err = c.PlanOccurrences(slot(t, c, "2026-11-01T00:30"), instant(t, "2026-11-01T06:15:00Z"), workers.Coalesce, 0, 1)
	if err != nil || len(plan.Occurrences) != 1 || !plan.Occurrences[0].Equal(instant(t, "2026-11-01T05:30:00Z")) || plan.NextLocal != "2026-11-01T02:30" || plan.Skipped != 1 {
		t.Fatal("lost first-fold occurrence or duplicated overlap", plan, err)
	}
}

func TestCalendarPlanMonthBacklogSkipLateAndFuture(t *testing.T) {
	spec := daily("UTC", "09:00")
	spec.Frequency, spec.DayOfMonth = "monthly", 31
	c := calendar(t, spec)
	start := slot(t, c, "1000-01-31T09:00")
	now := instant(t, "9998-12-31T10:00:00Z")
	plan, err := c.PlanOccurrences(start, now, workers.Coalesce, 0, 1)
	if err != nil || plan.Skipped != (9998-1000)*12+11 || plan.NextLocal != "9999-01-31T09:00" || len(plan.Occurrences) != 1 {
		t.Fatal("wide backlog did not coalesce arithmetically", plan, err)
	}
	plan, err = c.PlanOccurrences(start, now, workers.SkipLate, 30*time.Minute, 1)
	if err != nil || len(plan.Occurrences) != 0 || plan.Skipped != (9998-1000)*12+12 {
		t.Fatal(plan, err)
	}
	plan, err = c.PlanOccurrences(start, now, workers.SkipLate, time.Hour, 1)
	if err != nil || len(plan.Occurrences) != 1 {
		t.Fatal("inclusive lateness boundary", plan, err)
	}
	future := slot(t, c, "9999-01-31T09:00")
	plan, err = c.PlanOccurrences(future, now, workers.CatchUp, 0, 10)
	if err != nil || plan.NextLocal != future.String() || len(plan.Occurrences) != 0 || plan.Skipped != 0 {
		t.Fatal(plan, err)
	}
	for _, policy := range []workers.MissedRunPolicy{workers.CatchUp, workers.Coalesce, workers.SkipLate} {
		if _, err := c.PlanOccurrences(start, now, policy, -time.Second, 1); err == nil {
			t.Fatal("negative window accepted")
		}
	}
	if _, err := c.PlanOccurrences(start, now, workers.CatchUp, 0, 1001); err == nil {
		t.Fatal("unbounded limit accepted")
	}
}

func TestCalendarInitialGapAndCalendarPoliciesAgreeWithEnumeration(t *testing.T) {
	apia := calendar(t, daily("Pacific/Apia", "09:00"))
	first, at, skipped, err := apia.First(slot(t, apia, "2011-12-30T09:00"))
	if err != nil || skipped != 1 || first.String() != "2011-12-31T09:00" || !at.Equal(instant(t, "2011-12-30T19:00:00Z")) {
		t.Fatal(first, at, skipped, err)
	}
	for _, zone := range []string{"UTC", "America/New_York", "Australia/Lord_Howe", "Pacific/Apia", "America/Los_Angeles"} {
		for _, frequency := range []string{"hourly", "daily", "weekly", "monthly"} {
			spec := daily(zone, "02:30")
			spec.Frequency = frequency
			switch frequency {
			case "hourly":
				spec.TimeOfDay, spec.MinuteOfHour = "", 30
			case "weekly":
				spec.DayOfWeek = "sunday"
			case "monthly":
				spec.DayOfMonth = 31
			}
			c := calendar(t, spec)
			// Includes spring-forward, fall-back and UTC/local weekday edges.
			for _, text := range []string{"2026-03-10T10:00:00Z", "2026-11-01T06:15:00Z", "2026-10-05T02:00:00Z"} {
				now := instant(t, text)
				anchor, err := c.floorCivil(now.UTC().AddDate(0, -1, 0))
				if err != nil {
					t.Fatal(err)
				}
				anchor, _, _, err = c.First(anchor)
				if err != nil {
					t.Fatal(err)
				}
				all, err := c.PlanOccurrences(anchor, now, workers.CatchUp, 0, 1000)
				if err != nil || all.MoreDue || len(all.Occurrences) == 0 {
					t.Fatal(zone, frequency, all, err)
				}
				coalesced, err := c.PlanOccurrences(anchor, now, workers.Coalesce, 0, 1)
				if err != nil || !reflect.DeepEqual(coalesced.Occurrences, all.Occurrences[len(all.Occurrences)-1:]) || coalesced.NextLocal != all.NextLocal || !coalesced.NextAt.Equal(all.NextAt) || coalesced.Skipped != all.Skipped+int64(len(all.Occurrences)-1) {
					t.Fatal(zone, frequency, text, all, coalesced, err)
				}
			}
		}
	}
}
