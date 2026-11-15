package tmux

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	for dir, want := range map[string]string{
		"/home/me/dev/fndr":      "fndr",
		"/home/me/dev/fndr.nvim": "fndr_nvim",
		"/home/me/a:b":           "a_b",
		"/home/me/мой проект":    "мой проект",
		"/":                      "root",
	} {
		if got := Name(dir); got != want {
			t.Errorf("Name(%q) = %q, want %q", dir, got, want)
		}
	}
	a := hashed("/a/api")
	if a == hashed("/b/api") || a != hashed("/a/api") {
		t.Error("hashed names: not apart, or not the same each time")
	}
}

// fake is a tmux server in a map: session name → its folder.
type fake struct {
	sessions map[string]string
	typed    map[string][]string
	calls    [][]string
}

func (f *fake) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	name := func(t string) string { return strings.TrimSuffix(strings.TrimPrefix(t, "="), ":") }
	switch args[0] {
	case "list-sessions":
		if len(f.sessions) == 0 {
			return "", errors.New("no server running")
		}
		var lines []string
		for n, dir := range f.sessions {
			lines = append(lines, n+"\t"+dir)
		}
		return strings.Join(lines, "\n"), nil
	case "new-session":
		f.sessions[args[3]] = args[5]
	case "send-keys":
		f.typed[name(args[2])] = append(f.typed[name(args[2])], args[3])
	}
	return "", nil
}

// TestOpen: a session is made once, with the command typed in it, and
// found again after; another folder of the same name gets its own.
func TestOpen(t *testing.T) {
	f := &fake{sessions: map[string]string{}, typed: map[string][]string{}}
	for range 2 {
		if name, err := Open(f.run, "/dev/api", "ve"); err != nil || name != "api" {
			t.Fatalf("Open: %q, %v", name, err)
		}
	}
	if !slices.Equal(f.typed["api"], []string{"ve"}) {
		t.Errorf("typed %q: want ve, once", f.typed["api"])
	}
	other, err := Open(f.run, "/work/api", "")
	if err != nil || other != hashed("/work/api") || f.sessions[other] != "/work/api" {
		t.Errorf("another api: %q, %v", other, err)
	}
	if len(f.typed[other]) != 0 {
		t.Error("typed with no command")
	}
	if again, _ := Open(f.run, "/work/api", ""); again != other {
		t.Errorf("the other api again: %q, want %q", again, other)
	}
}

func TestOpenNoTmux(t *testing.T) {
	none := func(...string) (string, error) { return "", ErrNoTmux }
	if _, err := Open(none, "/dev/api", ""); !errors.Is(err, ErrNoTmux) {
		t.Errorf("err %v", err)
	}
}

// TestOpenRealTmux: the commands as the real tmux reads them, on a server
// of the test's own, in a folder of its own: the user's tmux is never
// touched, whatever $TMUX says.
func TestOpenRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// A socket path is at most some 100 bytes: TempDir's are longer.
	sockDir, err := os.MkdirTemp("/tmp", "fndr")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sockDir, "s")
	t.Setenv("TMUX", "")
	r := func(args ...string) (string, error) {
		return Exec(append([]string{"-S", sock, "-f", "/dev/null"}, args...)...)
	}
	t.Cleanup(func() {
		_, _ = r("kill-server")
		_ = os.RemoveAll(sockDir)
	})

	dir := filepath.Join(t.TempDir(), "my.proj")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	dir, _ = filepath.EvalSymlinks(dir) // as fndr has it: /tmp is /private/tmp on macOS
	name, err := Open(r, dir, "touch typed")
	if err != nil || name != "my_proj" {
		t.Fatalf("Open: %q, %v", name, err)
	}
	if again, err := Open(r, dir, "touch twice"); err != nil || again != name {
		t.Fatalf("again: %q, %v", again, err)
	}
	// The command runs in the folder, at the session's shell.
	deadline := time.Now().Add(5 * time.Second)
	for !exists(filepath.Join(dir, "typed")) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !exists(filepath.Join(dir, "typed")) {
		t.Error("the command was not run in the folder")
	}
	time.Sleep(300 * time.Millisecond)
	if exists(filepath.Join(dir, "twice")) {
		t.Error("the command was typed again in a session that was there")
	}
	// A session "my_proj2" must not be taken for "my_proj".
	if _, err := r("new-session", "-d", "-s", "x_proj_long", "-c", dir); err != nil {
		t.Fatal(err)
	}
	if got, err := Open(r, filepath.Join(filepath.Dir(dir), "x_proj"), ""); err != nil || got != "x_proj" {
		t.Errorf("a prefix of another session: %q, %v", got, err)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
