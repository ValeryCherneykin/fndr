package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// A plan with copies can take minutes: a folder of photos, a disk image.
// While y carries it out, the plan box shows what is copied so far, in
// place of its keys, and esc stops it: the copy under way is removed
// (mutation.ApplyContext), what was done before it stays, and the status
// says how far it got. The numbers are mutation.Progress, counted by the
// copy and read here on a tick: the copy never waits for the screen.

// progressEvery is how often the box is drawn again while copying: often
// enough to look alive, rarely enough to cost nothing.
const progressEvery = 100 * time.Millisecond

// progressTickMsg asks for the box to be drawn again.
type progressTickMsg struct{}

func progressTick() tea.Cmd {
	return tea.Tick(progressEvery, func(time.Time) tea.Msg { return progressTickMsg{} })
}

// progressTicked keeps the ticks coming while the plan is carried out;
// each one ends in a new frame, with the numbers as they are now.
func (m *Model) progressTicked() tea.Cmd {
	if m.oil == nil || !m.oil.applying {
		return nil
	}
	return progressTick()
}

// copies reports whether the plan copies anything: only then is there
// progress worth showing.
func (st *oilState) copies() bool {
	for _, op := range st.plan {
		if op.Kind == mutation.OpCopy {
			return true
		}
	}
	return false
}

// copyTotal is how many entries the plan's copies hold, each folder with
// all below it, or -1 while a folder is not counted yet, or holds more
// than is counted.
func (st *oilState) copyTotal() int64 {
	var total int64
	for _, op := range st.plan {
		if op.Kind != mutation.OpCopy {
			continue
		}
		if !st.isDir(op.From) {
			total++
			continue
		}
		n, ok := st.trashCounts[op.From]
		if !ok || n >= trashLimit {
			return -1
		}
		total += int64(n) + 1 // what it holds, and itself
	}
	return total
}

// isDir reports whether name, as a step of the plan names it, is a folder.
func (st *oilState) isDir(name string) bool {
	for i, it := range st.items {
		if st.entries[i].IsDir && (it.Name == name+"/" || it.Name == name) {
			return true
		}
	}
	return false
}

// progressLine is the plan box's last line while copying.
func (st *oilState) progressLine() string {
	if st.stopping {
		return stPlanVerb.Render("stopping…")
	}
	done := st.progress.Entries.Load()
	count := fmt.Sprint(done)
	if total := st.copyTotal(); total >= 0 {
		count = fmt.Sprintf("%d of %d", min(done, total), total)
	}
	return stPlanTitle.Render("copying ") + stName.Render(count+" · "+humanSize(st.progress.Bytes.Load())) +
		stPlanVerb.Render("   ") + stPlanTitle.Render("[esc]") + stPlanVerb.Render(" stop")
}
