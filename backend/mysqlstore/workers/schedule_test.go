package workers

import (
	"testing"
	"time"
)

func TestOccurrencePlanning(t *testing.T) {
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	now := start.Add(3250 * time.Millisecond)
	for _, test := range []struct {
		policy  MissedRunPolicy
		window  time.Duration
		count   int
		next    time.Time
		skipped int64
		more    bool
	}{{CatchUp, 0, 2, start.Add(2 * time.Second), 0, true}, {Coalesce, 0, 1, start.Add(4 * time.Second), 3, false}, {SkipLate, 300 * time.Millisecond, 1, start.Add(4 * time.Second), 3, false}, {SkipLate, 100 * time.Millisecond, 0, start.Add(4 * time.Second), 4, false}} {
		plan, err := PlanOccurrences(IntervalSchedule{start, time.Second, test.policy, test.window}, now, 2)
		if err != nil || len(plan.Occurrences) != test.count || !plan.NextAt.Equal(test.next) || plan.Skipped != test.skipped || plan.MoreDue != test.more {
			t.Fatalf("policy=%s plan=%+v err=%v", test.policy, plan, err)
		}
		if test.policy == Coalesce && !plan.Occurrences[0].Equal(start.Add(3*time.Second)) {
			t.Fatal("coalesced occurrence lost scheduled instant")
		}
	}
	// Advancing through bounded pages never loses or duplicates due identities.
	s := IntervalSchedule{NextAt: start, Interval: time.Second, Policy: CatchUp}
	var all []time.Time
	for {
		plan, err := PlanOccurrences(s, now, 2)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, plan.Occurrences...)
		s.NextAt = plan.NextAt
		if !plan.MoreDue {
			break
		}
	}
	if len(all) != 4 {
		t.Fatal("lost catch-up occurrences")
	}
	for i, at := range all {
		if !at.Equal(start.Add(time.Duration(i) * time.Second)) {
			t.Fatal("unstable occurrence identity")
		}
	}
	plan, err := PlanOccurrences(s, now, 2)
	if err != nil || len(plan.Occurrences) != 0 || !plan.NextAt.Equal(s.NextAt) {
		t.Fatal("watermark replay created duplicate occurrences")
	}
	// Very large backlog remains a bounded allocation/loop, not a drain loop.
	plan, err = PlanOccurrences(IntervalSchedule{NextAt: time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), Interval: time.Microsecond, Policy: CatchUp}, now, 3)
	if err != nil || len(plan.Occurrences) != 3 || !plan.MoreDue {
		t.Fatalf("unbounded backlog plan: %+v %v", plan, err)
	}
}

func TestOccurrencePlanningRejectsAmbiguousConfiguration(t *testing.T) {
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	valid := IntervalSchedule{NextAt: start, Interval: time.Second, Policy: CatchUp}
	for _, change := range []func(*IntervalSchedule){func(s *IntervalSchedule) { s.Policy = "" }, func(s *IntervalSchedule) { s.Interval = 0 }, func(s *IntervalSchedule) { s.Interval = time.Nanosecond }, func(s *IntervalSchedule) { s.NextAt = start.Add(time.Nanosecond) }, func(s *IntervalSchedule) { s.LatenessWindow = time.Second }, func(s *IntervalSchedule) { s.Policy = SkipLate; s.LatenessWindow = time.Second }, func(s *IntervalSchedule) { s.Policy = SkipLate; s.LatenessWindow = -1 }} {
		s := valid
		change(&s)
		if _, err := PlanOccurrences(s, start, 1); err == nil {
			t.Fatal("invalid schedule accepted")
		}
	}
	for _, limit := range []int{0, 1001} {
		if _, err := PlanOccurrences(valid, start, limit); err == nil {
			t.Fatal("unbounded occurrence limit accepted")
		}
	}
	end := time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)
	if _, err := PlanOccurrences(IntervalSchedule{NextAt: end, Interval: time.Second, Policy: Coalesce}, end, 1); err == nil {
		t.Fatal("out-of-range watermark accepted")
	}
}
