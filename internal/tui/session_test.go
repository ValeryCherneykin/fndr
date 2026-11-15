package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
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

// TestSessionMakesProject: a name no folder found holds is offered as a
// new project; y makes it, opens its session there and quits, and the
// folder is in the index.
func TestSessionMakesProject(t *testing.T) {
	m, root, f := sessionModel(t, true)
	typeText(m, "kafka-demo")
	if quit := key(m, 's', tea.ModCtrl); quit || m.making != "kafka-demo" || !strings.Contains(m.notice, "kafka-demo/") {
		t.Fatalf("offer: quit %v, making %q, notice %q", quit, m.making, m.notice)
	}
	if len(f.calls) != 0 || exists(filepath.Join(root, "kafka-demo")) {
		t.Fatalf("made before y: calls %q", f.calls)
	}
	if quit := key(m, 'y', 0); !quit {
		t.Fatalf("y did not quit: notice %q", m.notice)
	}
	dir := filepath.Join(root, "kafka-demo")
	if !exists(dir) {
		t.Fatal("no folder made")
	}
	if c := f.did("new-session"); !slices.Equal(c, []string{"new-session", "-d", "-s", "kafka-demo", "-c", dir}) {
		t.Errorf("new-session %q", c)
	}
	if !slices.ContainsFunc(m.local.ix.Snapshot(), func(e indexer.Entry) bool { return e.Path == dir && e.IsDir }) {
		t.Error("the folder made is not in the index")
	}
}

// TestSessionPartNameOpens: part of a name, as typed every day, opens the
// selection at once; nothing is offered or made.
func TestSessionPartNameOpens(t *testing.T) {
	m, root, f := sessionModel(t, true)
	typeText(m, "sr")
	if quit := key(m, 's', tea.ModCtrl); !quit || m.making != "" {
		t.Fatalf("quit %v, making %q, notice %q", quit, m.making, m.notice)
	}
	if c := f.did("new-session"); len(c) < 6 || c[5] != filepath.Join(root, "src") {
		t.Errorf("new-session %q", c)
	}
}

// TestSessionOfferAnswers: enter opens the selection instead of making the
// name; a paste drops the offer, and the y after it makes nothing.
func TestSessionOfferAnswers(t *testing.T) {
	m, root, f := sessionModel(t, true)
	typeText(m, "smn") // src/main.go, but not whole
	key(m, 's', tea.ModCtrl)
	if m.making == "" {
		t.Fatal("no offer")
	}
	if quit := send(m, tea.KeyPressMsg{Code: tea.KeyEnter}); !quit {
		t.Fatalf("enter did not open: notice %q", m.notice)
	}
	if c := f.did("new-session"); len(c) < 6 || c[5] != filepath.Join(root, "src") || exists(filepath.Join(root, "smn")) {
		t.Errorf("new-session %q, want src and nothing made", c)
	}

	m, root, f = sessionModel(t, true)
	typeText(m, "smn")
	key(m, 's', tea.ModCtrl)
	send(m, tea.PasteMsg{Content: "x"}) // a paste is typing: the offer is gone, y after it is a letter
	if quit := key(m, 'y', 0); quit || m.making != "" || len(f.calls) != 0 || exists(filepath.Join(root, "smn")) {
		t.Errorf("paste, y: quit %v, making %q, calls %q", quit, m.making, f.calls)
	}
}

// TestMakeProject: a folder already there is opened, as dev-new does; a
// file in its place is an error; "a/b" makes both, for the index.
func TestMakeProject(t *testing.T) {
	_, root := browseModel(t, "")
	if dir, made, err := makeProject(root, "docs"); err != nil || dir != filepath.Join(root, "docs") || made != nil {
		t.Errorf("there already: %q, %v, %v", dir, made, err)
	}
	if _, _, err := makeProject(root, "README.md"); !errors.Is(err, mutation.ErrExists) {
		t.Errorf("a file there: %v", err)
	}
	dir, made, err := makeProject(root, "work/api")
	if err != nil || dir != filepath.Join(root, "work", "api") || len(made) != 2 {
		t.Errorf("work/api: %q, %d made, %v", dir, len(made), err)
	}
	if _, _, err := makeProject(root, "../out"); !errors.Is(err, mutation.ErrOutside) {
		t.Errorf("../out: %v", err)
	}
}

// TestSessionMadeButTmuxSaysNo: the folder made stays found, and the
// status says it was made.
func TestSessionMadeButTmuxSaysNo(t *testing.T) {
	m, root, f := sessionModel(t, true)
	f.fail = "new-session"
	typeText(m, "kafka-demo")
	key(m, 's', tea.ModCtrl)
	if quit := key(m, 'y', 0); quit || !m.noticeErr || !strings.HasPrefix(m.notice, "made ") {
		t.Fatalf("quit %v, notice %q", quit, m.notice)
	}
	dir := filepath.Join(root, "kafka-demo")
	if !slices.ContainsFunc(m.local.ix.Snapshot(), func(e indexer.Entry) bool { return e.Path == dir }) {
		t.Error("made, but not found")
	}
}

// TestSessionOfferEsc: esc says no, quietly, and fndr stays as it was.
func TestSessionOfferEsc(t *testing.T) {
	m, root, f := sessionModel(t, true)
	typeText(m, "kafka-demo")
	key(m, 's', tea.ModCtrl)
	if quit := send(m, tea.KeyPressMsg{Code: tea.KeyEscape}); quit || m.making != "" || m.query != "kafka-demo" || len(f.calls) != 0 || exists(filepath.Join(root, "kafka-demo")) {
		t.Errorf("esc: quit %v, making %q, query %q, calls %q", quit, m.making, m.query, f.calls)
	}
}
