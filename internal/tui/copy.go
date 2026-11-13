package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/clipboard"
)

// Copying a path is a chord, as the config's binds are: ctrl+y, then y the
// selected entry's full path, r its path from the folder searched, as the
// row shows it, n its name. The status bar shows the choices after ctrl+y,
// so nothing is remembered; with ctrl held all along it works the same
// (ctrl+y ctrl+r). gp in OIL copies the line's full path. The status bar
// says what was copied and by which way (package clipboard), or why
// nothing was.
//
// A chord, not ctrl+shift+y: tmux, unless set up for it, and older
// terminals send ctrl+shift+y as ctrl+y, and the short path would
// silently be the full one. Nor ctrl+y twice: a key meaning one thing or
// another by what came before is not one the hand learns.

// copyHint is the status bar while a copy chord waits.
const copyHint = "ctrl+y   y path   r from here   n name"

// copier copies text the way it can here: the tools to run, and OSC 52 to
// send. A field, so tests never touch the clipboard of whoever runs them.
type copier struct {
	tools []clipboard.Tool
	osc52 bool
}

// systemCopier is the copier of the machine fndr runs on.
func systemCopier() copier {
	tools, osc52 := clipboard.Tools(clipboard.Env{Getenv: os.Getenv, LookPath: exec.LookPath, GOOS: runtime.GOOS})
	return copier{tools: tools, osc52: osc52}
}

// copiedMsg says how a copy went.
type copiedMsg struct {
	text   string
	copied []string // the ways that worked
	errs   []error
}

// copyKey finishes the copy chord with k, a keystroke, ctrl held or not.
func (m *Model) copyKey(k string) tea.Cmd {
	path := m.cwd // nothing selected: the folder itself
	if len(m.results) > 0 {
		path = m.results[m.cursor].Entry.Path
	}
	switch strings.TrimPrefix(k, "ctrl+") {
	case "y":
		return m.copyText(path)
	case "r":
		short, err := filepath.Rel(m.under(), path)
		if err != nil || short == "." || strings.HasPrefix(short, "..") {
			m.setNotice("nothing below the folder to copy a path from", true)
			return nil
		}
		return m.copyText(short)
	case "n":
		return m.copyText(filepath.Base(path))
	case "esc":
		return nil
	}
	m.setNotice("ctrl+y "+k+" copies nothing: "+strings.TrimPrefix(copyHint, "ctrl+y   "), true)
	return nil
}

// copyText puts text on the clipboard, off the UI goroutine.
func (m *Model) copyText(text string) tea.Cmd {
	c := m.copier
	if len(c.tools) == 0 && !c.osc52 {
		m.setNotice("cannot copy: no clipboard here (pbcopy, wl-copy, xclip, xsel, clip.exe)", true)
		return nil
	}
	ctx := m.ctx
	run := func() tea.Msg {
		copied, errs := clipboard.Run(ctx, c.tools, text)
		return copiedMsg{text: text, copied: copied, errs: errs}
	}
	if !c.osc52 {
		return run
	}
	// OSC 52 goes to the terminal through Bubble Tea, which owns it; there
	// is no answer to tell whether the terminal did it.
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		msg, _ := run().(copiedMsg)
		msg.copied = append([]string{"osc52"}, msg.copied...)
		return msg
	})
}

// copied says in the status bar what was copied, and how.
func (m *Model) copied(msg *copiedMsg) {
	text := m.tildify(msg.text)
	switch {
	case len(msg.copied) == 0 && len(msg.errs) > 0:
		m.setNotice("cannot copy: "+msg.errs[0].Error(), true)
	case len(msg.errs) > 0:
		m.setNotice("copied "+text+" · "+strings.Join(msg.copied, " ")+" ("+msg.errs[0].Error()+")", true)
	default:
		m.setNotice("copied "+text+" · "+strings.Join(msg.copied, " "), false)
	}
}
