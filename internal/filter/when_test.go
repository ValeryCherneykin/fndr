package filter

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

func changed(rel string, at time.Time) indexer.Entry {
	return indexer.NewEntry(under+"/"+rel, 10, at.UnixNano(), false)
}

// when returns the names of es that tags keep in a search made at now.
func when(t *testing.T, es []indexer.Entry, now time.Time, tags ...string) []string {
	t.Helper()
	var s Stack
	for _, text := range tags {
		tag, err := Parse(text)
		if err != nil {
			t.Fatalf("Parse(%q): %v", text, err)
		}
		s = s.Toggle(tag)
	}
	keep := s.Keep(es, under, now)
	got := []string{}
	for _, e := range es {
		if keep(e) {
			got = append(got, strings.TrimPrefix(e.Path, under+"/"))
		}
	}
	return got
}

// TestWhen: days by the calendar. At 01:00, a file changed at 23:00, two
// hours ago, is yesterday's.
func TestWhen(t *testing.T) {
	zone := time.FixedZone("here", 3*3600)
	at := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2026, month, day, hour, minute, 0, 0, zone)
	}
	now := at(11, 12, 1, 0)
	es := []indexer.Entry{
		changed("late.go", at(11, 11, 23, 0)),
		changed("night.md", at(11, 12, 0, 30)),
		changed("midnight.txt", at(11, 12, 0, 0)), // today's first moment, not yesterday's last
		changed("third.txt", at(11, 10, 0, 0)),    // the first moment of @3d
		changed("monday.go", at(11, 6, 0, 0)),     // the first moment of @week
		changed("old.go", at(11, 5, 23, 59)),
		changed("future.go", at(12, 1, 0, 0)), // a clock set wrong
		indexer.NewEntry(under+"/new", 4096, at(11, 12, 0, 10).UnixNano(), true),
	}
	for _, tc := range []struct {
		tags []string
		want []string
	}{
		{[]string{"@today"}, []string{"night.md", "midnight.txt", "future.go", "new"}},
		{[]string{"@1d"}, []string{"night.md", "midnight.txt", "future.go", "new"}},
		{[]string{"@yesterday"}, []string{"late.go"}},
		{[]string{"@2d"}, []string{"late.go", "night.md", "midnight.txt", "future.go", "new"}},
		{[]string{"@3d"}, []string{"late.go", "night.md", "midnight.txt", "third.txt", "future.go", "new"}},
		{[]string{"@week"}, []string{"late.go", "night.md", "midnight.txt", "third.txt", "monday.go", "future.go", "new"}},
		{[]string{"@7d"}, []string{"late.go", "night.md", "midnight.txt", "third.txt", "monday.go", "future.go", "new"}},
		{[]string{"@week", ".go"}, []string{"late.go", "monday.go", "future.go"}},
		{[]string{"@today|@yesterday", "files"}, []string{"late.go", "night.md", "midnight.txt", "future.go"}},
		{[]string{"@yesterday>1k"}, []string{}}, // a size on a time
	} {
		if got := when(t, es, now, tc.tags...); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.tags, got, tc.want)
		}
	}
}

// TestWhenClockChange: a day is counted on the calendar, not as 24 hours.
// In New York, 8 March 2026 has 23; 24 hours back from midnight is still
// the 7th.
func TestWhenClockChange(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no time zone database:", err)
	}
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, ny)
	es := []indexer.Entry{
		changed("sunday.go", time.Date(2026, 3, 8, 0, 30, 0, 0, ny)),
		changed("saturday.go", time.Date(2026, 3, 7, 23, 30, 0, 0, ny)),
	}
	if got := when(t, es, now, "@yesterday"); !slices.Equal(got, []string{"sunday.go"}) {
		t.Errorf("@yesterday: %q, want sunday.go alone", got)
	}
}

// TestWhenAtEachSearch: midnight comes between two searches with the same
// tag, and the tag is never written to: a search in flight may hold it.
func TestWhenAtEachSearch(t *testing.T) {
	tag, err := Parse("@today")
	if err != nil {
		t.Fatal(err)
	}
	s := Stack{tag}
	evening := time.Date(2026, 11, 11, 23, 0, 0, 0, time.UTC)
	es := []indexer.Entry{changed("x", evening)}
	if !s.Keep(es, under, evening.Add(time.Hour/2))(es[0]) {
		t.Error("at 23:30, a file of 23:00 is today's")
	}
	if s.Keep(es, under, evening.Add(2*time.Hour))(es[0]) {
		t.Error("at 01:00, a file of 23:00 is yesterday's")
	}
	if tm := s[0].terms[0]; tm.after != 0 || tm.before != 0 {
		t.Errorf("Keep wrote into the tag: %+v", tm)
	}
}

// TestHasWhen: the folder where something was changed today.
func TestHasWhen(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	es := []indexer.Entry{
		dir("dev"), dir("dev/fndr"), dir("dev/old"),
		changed("dev/fndr/main.go", now.Add(-time.Hour)),
		changed("dev/old/main.go", now.AddDate(0, -1, 0)),
	}
	if got := when(t, es, now, "has:@today"); !slices.Equal(got, []string{"dev/fndr"}) {
		t.Errorf("has:@today: %q, want dev/fndr", got)
	}
}
