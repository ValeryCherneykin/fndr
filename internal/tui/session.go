package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
	"github.com/ValeryCherneykin/fndr/internal/search"
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
	made      []indexer.Entry // folders ctrl+s made for the session, for the index
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
// program to run. A query that the selection does not hold whole names a
// project that is not there yet ("kafka-demo", with kafka selected): then
// ctrl+s offers to make it first, the scripts' dev-new. Typing part of a
// name ("fnd" for fndr) opens at once, as picking is what ctrl+s is
// pressed for every day, and making a folder is never done unasked.
func (m *Model) openSession() tea.Cmd {
	switch {
	case !m.tmuxOn:
		m.setNotice("tmux is off: tmux = on in the config turns ctrl+s on", false)
		return nil
	case m.tmuxCmd == nil:
		m.setNotice(tmux.ErrNoTmux.Error(), true)
		return nil
	}
	if name := m.newProject(); name != "" {
		m.making = name
		hint := "create " + m.tildify(filepath.Join(m.cwd, name)) + "/ and open its session?   y yes"
		if len(m.results) > 0 {
			hint += "   enter " + m.results[m.cursor].Entry.Name()
		}
		m.setNotice(safeText(hint)+"   esc no", false)
		return nil
	}
	return m.startSession(m.sessionDir(), "")
}

// newProject is the folder ctrl+s offers to make, or "": the query, when
// the selection's path below the folder searched does not hold it whole.
func (m *Model) newProject() string {
	text := strings.TrimSuffix(strings.TrimSpace(m.query), "/") // "x/" searches folders only; "" is returned as it is
	if len(m.results) > 0 && search.ContainsWhole(m.results[m.cursor].Entry.Path, m.under(), text) {
		return ""
	}
	return text
}

// makeKey answers the offer of openSession: y makes the folder and opens
// its session, enter opens the selection's instead, anything else drops
// it, as esc does a chord.
func (m *Model) makeKey(msg tea.KeyPressMsg) tea.Cmd {
	name := m.making
	m.making = ""
	switch msg.String() {
	case "y":
		return m.startSession(m.cwd, name)
	case "enter":
		if len(m.results) > 0 {
			return m.startSession(m.sessionDir(), "")
		}
	}
	return nil
}

// startSession opens the session of dir, first making the folder name in
// it if name is not "". A folder of that name already there is opened, as
// dev-new does; a file there is an error.
func (m *Model) startSession(dir, name string) tea.Cmd {
	r, run, inside := m.tmuxCmd, m.tmuxRun, m.tmux
	if name != "" {
		m.setNotice("making "+safeText(m.tildify(filepath.Join(dir, name)))+"/…", false)
	} else {
		m.setNotice("opening a session for "+safeText(m.tildify(dir))+"…", false)
	}
	return func() tea.Msg {
		var made []indexer.Entry
		if name != "" {
			var err error
			if dir, made, err = makeProject(dir, name); err != nil {
				return sessionMsg{err: err}
			}
		}
		s, err := tmux.Open(r, dir, run)
		if err == nil && inside {
			err = tmux.Switch(r, s)
		}
		return sessionMsg{dir: dir, name: s, made: made, err: err}
	}
}

// makeProject makes the folder name in dir, and the folders on the way
// ("work/api"), and returns it with the entries made, for the index. One
// that is there already is returned as it is, with none.
func makeProject(dir, name string) (string, []indexer.Entry, error) {
	c, err := mutation.Create(dir, name+"/")
	if errors.Is(err, mutation.ErrExists) {
		path := filepath.Join(dir, filepath.Clean(name))
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return path, nil, nil
		}
		return "", nil, err
	}
	if err != nil {
		return "", nil, err
	}
	var made []indexer.Entry
	for p := c.Path; ; p = filepath.Dir(p) {
		if info, err := os.Lstat(p); err == nil {
			made = append(made, indexer.NewEntry(p, info.Size(), info.ModTime().UnixNano(), true))
		}
		if p == c.Top {
			break
		}
	}
	return c.Path, made, nil
}

// sessionOpened quits fndr into the session, or says why not. A folder
// made is in the index either way: if tmux said no, it stays found.
func (m *Model) sessionOpened(msg *sessionMsg) tea.Cmd {
	if len(msg.made) > 0 {
		m.updateIndexes(nil, msg.made)
	}
	if msg.err != nil {
		text := msg.err.Error()
		if len(msg.made) > 0 {
			text = "made " + m.tildify(msg.dir) + ", but " + text
		}
		m.setNotice(safeText(text), true)
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
