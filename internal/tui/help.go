package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/config"
)

// The help page (F1): the whole screen, scrolled, written from the keys as
// the config left them, so what it says is what the keys do. A key moved
// in the file moves here; the user's own tag chords are listed with them.

// helpState is the help page while it is open; nil, it is closed.
type helpState struct {
	scroll int // the first line shown
	lines  []helpLine
}

func (h *helpState) clamp(page int) {
	h.scroll = min(max(h.scroll, 0), max(len(h.lines)-page, 0))
}

// helpLine is one line of the page: a key column and what it does, or,
// with no key, a heading or a line of prose.
type helpLine struct {
	key, text string
	heading   bool
}

// helpKeyWidth is the key column: wide enough for "ctrl+n down" and the
// like, so what they do starts in one column.
const helpKeyWidth = 22

// helpLines writes the page from the model's keys and chords.
func (m *Model) helpLines() []helpLine {
	var ls []helpLine
	head := func(s string) {
		if len(ls) != 0 {
			ls = append(ls, helpLine{})
		}
		ls = append(ls, helpLine{text: s, heading: true})
	}
	say := func(key, text string) { ls = append(ls, helpLine{key: key, text: text}) }

	head("Search")
	say("type", "letters in order, anywhere in the path: kfk finds kafka")
	say("a/", `a "/" at the end: folders only`)
	say("", "the folder you are in is searched; "+m.keysOf("global")+" for the whole home")

	for _, a := range config.Actions {
		if a.Group != "" {
			head(a.Group)
		}
		say(m.keysOf(a.Name), a.Doc)
	}
	say("backspace", "Erase the character before the cursor.")

	if len(m.binds) > 0 {
		head("Your chords")
		for _, b := range m.binds {
			say(chord(b.keys), "tag "+b.tag.String())
		}
	}

	head("Tags · type one, then " + m.keysOf("tag"))
	say(".go  .tar.gz", "by extension")
	say("dirs  files", "folders, or files only")
	say("image audio video", "doc code archive too: by kind")
	say(">1mb  <10k  .go>1mb", "by size, alone or with the rest")
	say("@today  @yesterday", "@3d @week: changed since")
	say("has:.json", "folders holding one, the deepest; by name: has:docker is a file named so")
	say("a|b", "either: .go|dirs")
	say("", "several tags: all of them. The same tag again takes it off.")

	head("OIL · " + m.keysOf("oil") + ", the folder or what was found as text")
	say("hjkl 0 ^ $ gg G", "move, as in Vim; counts too: 3j, 2dd")
	say("i a o O", "type; esc back to normal")
	say("cc  C  x", "rename: change the line, or part of it")
	say("dd", "to the trash")
	say("yy p", "copy, here or in any other folder")
	say("~/x  /x", "a line starting so moves the entry there")
	say("u  U", "undo, redo")
	say(":w", "show the plan; y applies it, n goes back")
	say("esc  q", "back to search; :q quits fndr")
	say("- _ g.", "up, the start folder, hidden files")

	head("Settings")
	if m.configPath != "" {
		say("", m.tildify(m.configPath))
	}
	say("", "every key and setting there, explained; fndr config prints a fresh one")
	say("", "alt keys on macOS need Option to be Alt: in Ghostty, macos-option-as-alt = left")
	return ls
}

// keysOf returns the keys of an action as the help shows them.
func (m *Model) keysOf(action string) string {
	if ks := m.actionKeys[action]; len(ks) > 0 {
		return strings.Join(ks, " ")
	}
	return "(off)"
}

// helpKey is handleKey while the help page is open: it scrolls, by the
// user's keys for moving, j and k as well, and closes on esc, q or the key
// that opened it.
func (m *Model) helpKey(msg tea.KeyPressMsg) tea.Cmd {
	page := m.listHeight()
	switch action, k := m.actionOf(msg), msg.String(); {
	case k == "ctrl+c":
		return m.quit()
	case k == "esc", k == "q", action == "help", action == "back":
		m.help = nil
	case action == "down", k == "j":
		m.help.scroll++
	case action == "up", k == "k":
		m.help.scroll--
	case action == "page_down", k == "space":
		m.help.scroll += page
	case action == "page_up":
		m.help.scroll -= page
	case action == "first", k == "g":
		m.help.scroll = 0
	case action == "last", k == "G":
		m.help.scroll = len(m.help.lines)
	}
	if m.help != nil {
		m.help.clamp(page)
	}
	return nil
}

// viewHelp draws the page: a title, the hairline, the lines that fit, and
// the status bar's place saying how to get about.
func (m *Model) viewHelp() tea.View {
	st, a := m.help, m.accent()
	h, w := m.listHeight(), m.width
	var b strings.Builder
	title := a.mode.Render(" HELP ")
	if m.icons {
		title += a.barClose
	}
	if room := w - ansi.StringWidth(title) - 2; room >= 8 && st.scroll < len(st.lines) && !st.lines[st.scroll].heading {
		for i := st.scroll - 1; i >= 0; i-- {
			if st.lines[i].heading {
				title += "  " + stPaneTitle.Render(ansi.Truncate(safeText(st.lines[i].text), room, "…"))
				break
			}
		}
	}
	b.WriteString(finish(title, w))
	b.WriteByte('\n')
	b.WriteString(stHair.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')

	keyWidth := min(helpKeyWidth, max((w-2)/3, 1))
	for i := range h {
		var line string
		if r := st.scroll + i; r < len(st.lines) {
			l := st.lines[r]
			switch {
			case l.heading:
				line = "  " + a.nameHit.Render(safeText(l.text))
			case l.key == "":
				line = "  " + stName.Render(safeText(l.text))
			default:
				key := ansi.Truncate(safeText(l.key), keyWidth-1, "…")
				gap := strings.Repeat(" ", max(keyWidth-ansi.StringWidth(key), 1))
				style := pick(l.key == "(off)", &stDir, &stChip)
				line = "  " + style.Render(key) + gap + stName.Render(safeText(l.text))
			}
		}
		b.WriteString(finish(line, w))
		b.WriteByte('\n')
	}
	bar := " j k scroll · esc closes"
	progress := fmt.Sprintf("%d–%d/%d ", st.scroll+1, min(st.scroll+h, len(st.lines)), len(st.lines))
	if gap := w - ansi.StringWidth(bar) - ansi.StringWidth(progress); gap > 0 {
		bar += strings.Repeat(" ", gap) + progress
	}
	b.WriteString(stBar.Render(finish(bar, w)))

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.ReportFocus = true
	v.WindowTitle = "fndr"
	return v
}
