package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// BenchmarkView: one frame of a full screen of results, the work done
// after every key before the frame can go out.
func BenchmarkView(b *testing.B) {
	paths := make([]string, 2000)
	for i := range paths {
		paths[i] = fmt.Sprintf("/r/dev/project%d/internal/pkg/file_%d.go", i%40, i)
	}
	m := testModel(b, paths...)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	typeText(m, "file")
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkKeySearch: a key typed with a query: the query line edited and
// a search started and answered, on 300k entries, as Update does it.
func BenchmarkKeySearch(b *testing.B) {
	paths := make([]string, 300_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("/r/dev/project%d/pkg%d/internal/file_%d.go", i%400, i%20, i)
	}
	m := testModel(b, paths...)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	b.ReportAllocs()
	for b.Loop() {
		typeText(m, "f")
		key(m, tea.KeyBackspace, 0)
	}
}

// BenchmarkTyping: a name typed key by key over 300k entries, each key's
// search answered and drawn, as the user sees it: narrowed by the key
// before (narrow.go), and searching the whole index every key, as before.
func BenchmarkTyping(b *testing.B) {
	paths := make([]string, 300_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("/r/dev/project%d/pkg%d/internal/file_%d.go", i%400, i%20, i)
	}
	for _, narrow := range []bool{true, false} {
		b.Run(fmt.Sprintf("narrow=%v", narrow), func(b *testing.B) {
			m := testModel(b, paths...)
			m.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
			b.ReportAllocs()
			keys := 0
			for b.Loop() {
				for _, r := range "file_7_99" {
					if !narrow {
						m.narrow = nil
					}
					typeText(m, string(r))
					_ = m.View()
					keys++
				}
				key(m, 'u', tea.ModCtrl)
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(keys), "µs/key")
		})
	}
}
