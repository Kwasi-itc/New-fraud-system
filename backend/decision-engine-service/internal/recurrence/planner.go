package recurrence

import (
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// Plan includes both the indexed physical watermark and its civil identity.
// Skipped includes DST gaps and missed occurrences; it never counts an overlap
// twice. Persist both cursors, occurrence receipts and jobs atomically.
type Plan struct {
	workers.OccurrencePlan
	NextLocal string
}

const maxCalendarSearch = 128

// First finds the first real occurrence at or after an explicit civil anchor.
// Gaps consume logical slots and are counted, never normalized to another time.
// Bounded searches fail closed for unsupported timezone transitions/ranges.
func (c *Calendar) First(start Slot) (Slot, time.Time, int64, error) {
	var skipped int64
	for i := 0; i < maxCalendarSearch; i++ {
		at, exists, err := c.Resolve(start)
		if err != nil {
			return Slot{}, time.Time{}, 0, err
		}
		if exists {
			return start, at, skipped, nil
		}
		start, err = c.Next(start)
		if err != nil {
			return Slot{}, time.Time{}, 0, err
		}
		skipped++
	}
	return Slot{}, time.Time{}, 0, fmt.Errorf("%w: calendar gap search bound", ErrCalendar)
}

// ValidatePolicy uses the shortest NOMINAL civil period, not a guessed elapsed
// interval. SkipLate still chooses only the latest due slot; its window is an
// elapsed lateness allowance, not a second occurrence or an overlap policy.
func (c *Calendar) ValidatePolicy(policy workers.MissedRunPolicy, window time.Duration) error {
	if c == nil || c.location == nil {
		return ErrCalendar
	}
	switch policy {
	case workers.CatchUp, workers.Coalesce:
		if window == 0 {
			return nil
		}
	case workers.SkipLate:
		minimum := map[string]time.Duration{"hourly": time.Hour, "daily": 24 * time.Hour, "weekly": 7 * 24 * time.Hour, "monthly": 28 * 24 * time.Hour}[c.spec.Frequency]
		if window >= 0 && window < minimum && window%time.Microsecond == 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: missed-run policy/window", ErrCalendar)
}

func calendarInstant(t time.Time) bool {
	u := t.UTC()
	return u.Year() >= 1000 && u.Year() <= 9999 && u.Nanosecond()%1000 == 0
}

// PlanOccurrences is pure and bounded; now must come from the database after
// locking the owner schedule. next is already a real, unprocessed civil slot
// (First initializes it). CatchUp emits at most limit occurrences. Coalesce and
// SkipLate seek in a fixed timezone-offset envelope, not through the backlog.
func (c *Calendar) PlanOccurrences(next Slot, now time.Time, policy workers.MissedRunPolicy, window time.Duration, limit int) (Plan, error) {
	if !calendarInstant(now) || limit < 1 || limit > 1000 {
		return Plan{}, ErrCalendar
	}
	if err := c.ValidatePolicy(policy, window); err != nil {
		return Plan{}, err
	}
	at, exists, err := c.Resolve(next)
	if err != nil || !exists {
		return Plan{}, ErrCalendar
	}
	out := Plan{OccurrencePlan: workers.OccurrencePlan{NextAt: at}, NextLocal: next.String()}
	if at.After(now) {
		return out, nil
	}
	advance := func(s Slot) (Slot, time.Time, int64, error) {
		n, err := c.Next(s)
		if err != nil {
			return Slot{}, time.Time{}, 0, err
		}
		return c.First(n)
	}
	if policy == workers.CatchUp {
		for len(out.Occurrences) < limit && !at.After(now) {
			out.Occurrences = append(out.Occurrences, at)
			previous := at
			var skipped int64
			next, at, skipped, err = advance(next)
			if err != nil || !at.After(previous) {
				return Plan{}, ErrCalendar
			}
			out.Skipped += skipped
		}
		out.NextAt, out.NextLocal, out.MoreDue = at, next.String(), !at.After(now)
		return out, nil
	}
	last, lastAt, err := c.latestDue(now)
	if err != nil {
		return Plan{}, err
	}
	distance, err := c.Distance(next, last)
	if err != nil {
		return Plan{}, err
	}
	if policy == workers.Coalesce || now.Sub(lastAt) <= window {
		out.Occurrences = []time.Time{lastAt}
	}
	next, at, gaps, err := advance(last)
	if err != nil || !at.After(now) {
		return Plan{}, ErrCalendar
	}
	out.Skipped = distance + 1 - int64(len(out.Occurrences)) + gaps
	out.NextAt, out.NextLocal = at, next.String()
	return out, nil
}

// floorCivil uses a timezone-free label, so weekly calculations use the CIVIL
// weekday. It also permits a constant-size search even after centuries offline.
func (c *Calendar) floorCivil(wall time.Time) (Slot, error) {
	t := time.Date(wall.Year(), wall.Month(), wall.Day(), c.hour, c.minute, 0, 0, time.UTC)
	switch c.spec.Frequency {
	case "hourly":
		t = time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), c.minute, 0, 0, time.UTC)
	case "weekly":
		t = t.AddDate(0, 0, -(int(t.Weekday())-int(c.weekday)+7)%7)
	case "monthly":
		t = time.Date(wall.Year(), wall.Month(), min(c.spec.DayOfMonth, monthEnd(wall.Year(), wall.Month())), c.hour, c.minute, 0, 0, time.UTC)
	}
	s := Slot{t}
	if t.After(wall) {
		return c.Previous(s)
	}
	if !c.valid(s) {
		return Slot{}, ErrCalendar
	}
	return s, nil
}

func (c *Calendar) latestDue(now time.Time) (Slot, time.Time, error) {
	// Resolve rejects offsets outside (-24h,+24h). Every due civil label
	// therefore precedes UTC-now+24h. Starting from local now would MISS a
	// first-fold occurrence when the wall clock has repeated backwards.
	upper := now.UTC().Add(24 * time.Hour)
	if upper.Year() > 9999 {
		upper = time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)
	}
	s, err := c.floorCivil(upper)
	if err != nil {
		return Slot{}, time.Time{}, err
	}
	for i := 0; i < maxCalendarSearch; i++ {
		at, exists, err := c.Resolve(s)
		if err != nil {
			return Slot{}, time.Time{}, err
		}
		if exists && !at.After(now) {
			return s, at, nil
		}
		s, err = c.Previous(s)
		if err != nil {
			return Slot{}, time.Time{}, err
		}
	}
	return Slot{}, time.Time{}, fmt.Errorf("%w: due search bound", ErrCalendar)
}
