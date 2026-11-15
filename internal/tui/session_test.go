package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/tmux"
)

// tmuxFake records what fndr asks of tmux; it has no sessions, and says
// no to whatever fail names.
type tmuxFake struct {
	calls [][]string
	fail  string
}

func (f *tmuxFake) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if args[0] == f.fail {
		return "", errors.New("tmux " + args[0] + ": no")
	}
	if args[0] == "list-sessions" {
		return "", errors.New("no server running")
	}
	return "", nil
}

func (f *tmuxFake) did(cmd string) []string {
	for _, c := range f.calls {
		if c[0] == cmd {
			return c
		}
	}
	return nil
}

func sessionModel(t *testing.T, inside bool) (*Model, string, *tmuxFake) {
	t.Helper()
	m, root := browseModel(t, "")
	f := &tmuxFake{}
	m.tmuxOn, m.tmuxRun, m.tmuxCmd, m.tmux = true, "ve", f.run, inside
	return m, root, f
}

// TestSessionInsideTmux: ctrl+s on a folder makes its session, with the
// editor typed in it, switches to it, and quits.
func TestSessionInsideTmux(t *testing.T) {
	m, root, f := sessionModel(t, true)
	m.selectName(t, "src")
	if quit := key(m, 's', tea.ModCtrl); !quit {
		t.Fatalf("did not quit: notice %q", m.notice)
	}
	src := filepath.Join(root, "src")
	if c := f.did("new-session"); !slices.Equal(c, []string{"new-session", "-d", "-s", "src", "-c", src}) {
		t.Errorf("new-session %q", c)
	}
	if c := f.did("send-keys"); !slices.Contains(c, "ve") {
		t.Errorf("send-keys %q", c)
	}
	if c := f.did("switch-client"); !slices.Equal(c, []string{"switch-client", "-t", "=src"}) {
		t.Errorf("switch-client %q", c)
	}
	if m.ExecAfter() != nil {
		t.Errorf("inside tmux, fndr becomes %q", m.ExecAfter())
	}
}

// TestSessionOutsideTmux: outside, nothing to switch: fndr quits to
// become tmux attach. A file's session is its folder's.
func TestSessionOutsideTmux(t *testing.T) {
	m, root, f := sessionModel(t, false)
	m.selectName(t, "README.md")
	key(m, 's', tea.ModCtrl)
	if c := f.did("new-session"); len(c) < 6 || c[5] != root {
		t.Errorf("new-session %q, want the file's folder %s", c, root)
	}
	if f.did("switch-client") != nil || !slices.Equal(m.ExecAfter(), tmux.Attach(tmux.Name(root))) {
		t.Errorf("switch %q, becomes %q", f.did("switch-client"), m.ExecAfter())
	}
}

// TestSessionOffOrFailing: off in the config, ctrl+s says how to turn it
// on and asks nothing of tmux; tmux saying no is said in red, and fndr
// stays.
func TestSessionOffOrFailing(t *testing.T) {
	m, _, f := sessionModel(t, true)
	m.tmuxOn = false
	if quit := key(m, 's', tea.ModCtrl); quit || len(f.calls) != 0 || !strings.Contains(m.notice, "tmux = on") {
		t.Errorf("off: quit %v, calls %q, notice %q", quit, f.calls, m.notice)
	}
	m, _, f = sessionModel(t, true)
	f.fail = "new-session"
	if quit := key(m, 's', tea.ModCtrl); quit || !m.noticeErr || !strings.Contains(m.notice, "no") {
		t.Errorf("failing: quit %v, notice %q", quit, m.notice)
	}
	m, _, _ = sessionModel(t, true)
	m.tmuxCmd = nil
	if quit := key(m, 's', tea.ModCtrl); quit || m.notice != tmux.ErrNoTmux.Error() {
		t.Errorf("no tmux: quit %v, notice %q", quit, m.notice)
	}
}
