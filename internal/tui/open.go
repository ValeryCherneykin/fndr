package tui

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/opener"
)

// Opening a file takes two trips through Update. Deciding how to open it
// reads the file, so it runs in a command and reports back with openMsg.
// Update then either hands the terminal to the editor, which only the
// Bubble Tea runtime can do, or starts the system opener in the background.
// Either way the outcome comes back as openedMsg and lands in the status
// bar.
type (
	openMsg struct {
		name     string
		argv     []string
		terminal bool // a terminal editor: suspend the UI while it runs
	}
	openedMsg struct {
		name    string
		viaApp  bool // opened by the system opener, which returns at once
		program bool // a native executable, deliberately not opened
		err     error
	}
)

// openSelected opens the selected file: text in the editor, anything else
// in the system's default application. Folders are entered instead (see
// activate).
func (m *Model) openSelected() tea.Cmd {
	if len(m.results) == 0 {
		return nil
	}
	e := &m.results[m.cursor].Entry
	// Copied out of m: the command runs on another goroutine and must not
	// touch the model.
	path, editor := e.Path, m.editor
	return func() tea.Msg { return resolveOpen(path, editor) }
}

// resolveOpen decides how to open path. It reads the start of the file,
// so it must run in a command, never in Update.
func resolveOpen(path, editor string) tea.Msg {
	name := filepath.Base(path)
	kind, err := opener.Classify(path)
	if err != nil {
		return openedMsg{name: name, err: err}
	}
	if kind == opener.Program {
		return openedMsg{name: name, program: true}
	}
	if kind == opener.Text {
		argv, err := opener.Editor(editor)
		if err != nil {
			return openedMsg{name: name, err: err}
		}
		return openMsg{name: name, argv: append(argv, path), terminal: true}
	}
	argv, err := opener.System(path)
	if err != nil {
		return openedMsg{name: name, err: err}
	}
	return openMsg{name: name, argv: argv}
}

// run carries out an openMsg.
func (msg openMsg) run() tea.Cmd {
	cmd := exec.Command(msg.argv[0], msg.argv[1:]...)
	if msg.terminal {
		// ExecProcess releases the terminal, runs the editor in the
		// foreground, and restores the UI with its state intact when the
		// editor exits.
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return openedMsg{name: msg.name, err: err}
		})
	}
	return func() tea.Msg {
		// Output is captured, never let through: a stray line from
		// xdg-open would be drawn over the UI. On failure it is the most
		// useful part of the error.
		out, err := cmd.CombinedOutput()
		if err != nil {
			if line, _, _ := bytes.Cut(bytes.TrimSpace(out), []byte("\n")); len(line) > 0 {
				err = fmt.Errorf("%s: %s", msg.argv[0], line)
			}
		}
		return openedMsg{name: msg.name, viaApp: true, err: err}
	}
}

// opened reports the outcome of an open in the status bar. A successful
// return from the editor needs no report: the user just saw it.
func (m *Model) opened(msg openedMsg) {
	switch {
	case msg.err != nil:
		m.setNotice("cannot open "+msg.name+": "+msg.err.Error(), true)
	case msg.program:
		m.setNotice(msg.name+" is a program — not running it", false)
	case msg.viaApp:
		m.setNotice("opened "+msg.name, false)
	}
}

// setNotice shows a one-line report in the status bar until the next key.
//
// Notices quote names and errors, which quote paths, so the text is made
// safe here, once, for every caller.
func (m *Model) setNotice(text string, isErr bool) {
	m.notice, m.noticeErr = safeText(text), isErr
}
