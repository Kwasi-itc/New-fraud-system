package workers

import (
	"errors"
	"time"
)

type MissedRunPolicy string

const (
	CatchUp  MissedRunPolicy = "catch_up"
	Coalesce MissedRunPolicy = "coalesce"
	SkipLate MissedRunPolicy = "skip_late"
)

type IntervalSchedule struct {
	NextAt   time.Time
	Interval time.Duration
	Policy   MissedRunPolicy
	// SkipLate emits only the most recent due occurrence within this explicit
	// lateness window. Other policies require zero, rather than ignoring it.
	LatenessWindow time.Duration
}

type OccurrencePlan struct {
	Occurrences []time.Time
	NextAt      time.Time
	MoreDue     bool
	Skipped     int64
}

// PlanOccurrences is bounded interval arithmetic, not a durable scheduler. Now
// must be read from the database by the owner holding the schedule row lock.
// Persist each unique (schedule ID, occurrence instant), its job and the returned
// watermark in one transaction. Never save the watermark separately or hold the
// schedule lock over external work. RunOnStart/singleton reservation policy is
// owner-owned: this helper never invents startup occurrences or enables a kind.
func PlanOccurrences(schedule IntervalSchedule, now time.Time, limit int) (OccurrencePlan, error) {
	if !scheduleTime(schedule.NextAt) || !scheduleTime(now) || schedule.Interval < time.Microsecond || schedule.Interval%time.Microsecond != 0 || limit < 1 || limit > 1000 {
		return OccurrencePlan{}, errors.New("microsecond schedule/database time, positive interval and occurrence limit 1..1000 required")
	}
	switch schedule.Policy {
	case CatchUp, Coalesce:
		if schedule.LatenessWindow != 0 {
			return OccurrencePlan{}, errors.New("lateness window is only valid for skip_late")
		}
	case SkipLate:
		if schedule.LatenessWindow < 0 || schedule.LatenessWindow >= schedule.Interval || schedule.LatenessWindow%time.Microsecond != 0 {
			return OccurrencePlan{}, errors.New("skip_late requires a nonnegative microsecond window shorter than the interval")
		}
	default:
		return OccurrencePlan{}, errors.New("explicit missed-run policy required")
	}
	next, clock := schedule.NextAt.UTC().UnixMicro(), now.UTC().UnixMicro()
	result := OccurrencePlan{NextAt: schedule.NextAt.UTC()}
	if next > clock {
		return result, nil
	}
	interval := schedule.Interval.Microseconds()
	due := (clock-next)/interval + 1
	if schedule.Policy == CatchUp {
		n := min(due, int64(limit))
		result.Occurrences = make([]time.Time, 0, n)
		for i := int64(0); i < n; i++ {
			result.Occurrences = append(result.Occurrences, time.UnixMicro(next+i*interval).UTC())
		}
		result.NextAt = time.UnixMicro(next + n*interval).UTC()
		result.MoreDue = n < due
	} else {
		last := next + (due-1)*interval
		if schedule.Policy == Coalesce || clock-last <= schedule.LatenessWindow.Microseconds() {
			result.Occurrences = []time.Time{time.UnixMicro(last).UTC()}
		}
		result.Skipped = due - int64(len(result.Occurrences))
		result.NextAt = time.UnixMicro(last + interval).UTC()
	}
	if !scheduleTime(result.NextAt) {
		return OccurrencePlan{}, errors.New("schedule watermark exceeds supported DATETIME range")
	}
	return result, nil
}

func scheduleTime(t time.Time) bool {
	t = t.UTC()
	return t.Year() >= 1000 && t.Year() <= 9999 && t.Nanosecond()%1000 == 0
}
