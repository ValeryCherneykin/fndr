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
		reveal   bool // shown in the file manager, not opened (ctrl+g)
	}
	openedMsg struct {
		name     string
		viaApp   bool // opened by the system opener, which returns at once
		revealed bool // shown in the file manager
		program  bool // a native executable, deliberately not opened
		err      error
	}
)

// openSelected opens the selected file: text in the editor, anything else
// in the system's default application. Folders are entered instead (see
// activate).
func (m *Model) openSelected() tea.Cmd {
	if len(m.results) == 0 {
		return nil
	}
	return m.open(m.results[m.cursor].Entry.Path)
}

// open opens the file at path, as openSelected does.
func (m *Model) open(path string) tea.Cmd {
	// Copied out of m: the command runs on another goroutine and must not
	// touch the model.
	editor := m.editor
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
		return openedMsg{name: msg.name, viaApp: !msg.reveal, revealed: msg.reveal, err: err}
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
	case msg.revealed:
		m.setNotice("shown "+msg.name, false)
	}
}

// showInFiles shows path in the system's file manager (opener.Reveal), and fndr
// stays open: ctrl+g in search, gx in OIL. Only the command is built
// here; Update runs it as any system open.
func (m *Model) showInFiles(path string, isDir bool) tea.Cmd {
	name := filepath.Base(path)
	argv, err := opener.Reveal(path, isDir)
	if err != nil {
		m.setNotice("cannot show "+name+": "+err.Error(), true)
		return nil
	}
	return func() tea.Msg { return openMsg{name: name, argv: argv, reveal: true} }
}

// setNotice shows a one-line report in the status bar until the next key.
//
// Notices quote names and errors, which quote paths, so the text is made
// safe here, once, for every caller.
func (m *Model) setNotice(text string, isErr bool) {
	m.notice, m.noticeErr = safeText(text), isErr
}
