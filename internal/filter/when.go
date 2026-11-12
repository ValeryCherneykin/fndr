package filter

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

// A time tag is when the entry was last changed, counted in days on the
// calendar, the way people say it: @today is since midnight, so a file
// changed at 23:00 is yesterday's by 01:00. @yesterday is that whole day,
// @3d today and the two days before it, @week the same as @7d.
//
// A tag holds days, not times: fndr may stay open past midnight, and the
// days are turned into times for each search (Stack.Keep, with its now).

// maxDays bounds @Nd: enough for any memory of a file, small enough that
// going back that many days is a date and not an overflow.
const maxDays = 100 * 366

// parseWhen reads what follows @: today, yesterday, week, or a number of
// days, 3d.
func parseWhen(w string) (term, error) {
	tm := term{kind: kindTime, to: -1}
	switch w {
	case "today":
		tm.from = 0
	case "yesterday":
		tm.from, tm.to = 1, 0
	case "week":
		tm.from = 6
	default:
		num, ok := strings.CutSuffix(w, "d")
		n, err := strconv.Atoi(num)
		// Atoi takes a sign, "+3": only digits, as typed for a count.
		if !ok || err != nil || strings.TrimLeft(num, "0123456789") != "" || n < 1 || n > maxDays {
			return term{}, errors.New("time is @today, @yesterday, @week or a number of days: @3d")
		}
		tm.from = n - 1 // @1d is today
	}
	return tm, nil
}

// at returns s with the days of its time tags turned into times, for a
// search made at now; s itself if it has none, which is most searches.
func (s Stack) at(now time.Time) Stack {
	if !s.hasTime() {
		return s
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	out := make(Stack, len(s))
	for i, t := range s {
		t.terms = append([]term(nil), t.terms...) // never write into a tag a search may hold
		for j := range t.terms {
			tm := &t.terms[j]
			if tm.kind != kindTime {
				continue
			}
			// AddDate, not 24 hours a day: a day with a clock change has 23
			// or 25 of them.
			tm.after = midnight.AddDate(0, 0, -tm.from).UnixNano()
			tm.before = math.MaxInt64 // a clock set wrong can leave a file in the future
			if tm.to >= 0 {
				tm.before = midnight.AddDate(0, 0, -tm.to).UnixNano()
			}
		}
		out[i] = t
	}
	return out
}

func (s Stack) hasTime() bool {
	for _, t := range s {
		for _, tm := range t.terms {
			if tm.kind == kindTime {
				return true
			}
		}
	}
	return false
}
