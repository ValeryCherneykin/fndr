package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/tmux"
)

// ctrl+s: a tmux session for a folder, the project switcher's job (the
// user's scripts that fzf a folder of ~/dev into a session). Search finds
// the folder, ctrl+s opens its session, made the first time with the
// user's editor in it (tmux_run), gone to after. fndr then quits: the
// user is in the session now, and fndr left open in the pane behind would
// be in the way. Inside tmux the client switches; outside, fndr becomes
// "tmux attach" once it has quit (ExecAfter), and detaching from the
// session is the shell again.
//
// Off unless the config says tmux = on: a key that starts a tmux server
// is a surprise for whoever does not use tmux.

type sessionMsg struct {
	dir, name string
	err       error
}

// sessionDir is the folder ctrl+s opens: the selected one, the selected
// file's, or with nothing selected the folder fndr is in. Unlike ctrl+e,
// the selection counts while browsing too: in ~/dev, the project picked.
func (m *Model) sessionDir() string {
	if len(m.results) == 0 {
		return m.cwd
	}
	e := &m.results[m.cursor].Entry
	if e.IsDir {
		return e.Path
	}
	return filepath.Dir(e.Path)
}

// openSession opens the session of sessionDir, in a command: tmux is a
// program to run.
func (m *Model) openSession() tea.Cmd {
	switch {
	case !m.tmuxOn:
		m.setNotice("tmux is off: tmux = on in the config turns ctrl+s on", false)
		return nil
	case m.tmuxCmd == nil:
		m.setNotice(tmux.ErrNoTmux.Error(), true)
		return nil
	}
	dir, r, run, inside := m.sessionDir(), m.tmuxCmd, m.tmuxRun, m.tmux
	m.setNotice("opening a session for "+m.tildify(dir)+"…", false)
	return func() tea.Msg {
		name, err := tmux.Open(r, dir, run)
		if err == nil && inside {
			err = tmux.Switch(r, name)
		}
		return sessionMsg{dir: dir, name: name, err: err}
	}
}

// sessionOpened quits fndr into the session, or says why not.
func (m *Model) sessionOpened(msg *sessionMsg) tea.Cmd {
	if msg.err != nil {
		m.setNotice(msg.err.Error(), true)
		return nil
	}
	_ = m.learn(msg.dir) // saved on the way out, as ctrl+e's
	if !m.tmux {
		m.execAfter = tmux.Attach(msg.name)
	}
	return m.quit()
}

// ExecAfter is the command fndr is to become once it has quit, or nil:
// "tmux attach" for a session opened outside tmux.
func (m *Model) ExecAfter() []string { return m.execAfter }
