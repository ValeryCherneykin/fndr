package tui

import "strings"

// previewMinWidth is the narrowest terminal that gets a preview pane. Below
// it, halves would be too thin to read: the list stays whole even with the
// preview switched on.
const previewMinWidth = 100

// listWidth is how many cells of each row the result list takes: the whole
// width, or the left half when the preview pane is shown.
func (m *Model) listWidth() int {
	if m.preview && m.width >= previewMinWidth {
		return m.width / 2
	}
	return m.width
}

// paneShown reports whether the preview pane takes the right side.
func (m *Model) paneShown() bool { return m.listWidth() < m.width }

// viewHairline draws the line under the query. With the pane shown, a "┬"
// joins it to the pane's border, so the two read as one frame.
func (m *Model) viewHairline() string {
	if !m.paneShown() {
		return stHair.Render(strings.Repeat("─", m.width))
	}
	lw := m.listWidth()
	return stHair.Render(strings.Repeat("─", lw) + "┬" + strings.Repeat("─", m.width-lw-1))
}

// viewPaneLine appends line i of the preview pane, border included, after a
// list row. Without the pane it appends nothing.
func (m *Model) viewPaneLine(b *strings.Builder, i int) {
	if !m.paneShown() {
		return
	}
	_ = i // the pane's contents come with the next step
	b.WriteString(stHair.Render("│"))
	b.WriteString(strings.Repeat(" ", m.width-m.listWidth()-1))
}
