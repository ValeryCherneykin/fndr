package clipboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func env(goos string, vars map[string]string, onPath ...string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(p string) (string, error) {
			if slices.Contains(onPath, p) {
				return "/bin/" + p, nil
			}
			return "", errors.New("not found")
		},
		GOOS: goos,
	}
}

func names(ts []Tool) []string {
	ns := make([]string, len(ts))
	for i, t := range ts {
		ns[i] = t.Name
	}
	return ns
}

func TestTools(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   Env
		want  []string
		osc52 bool
	}{
		{"macOS", env("darwin", nil, "pbcopy"), []string{"pbcopy"}, false},
		{"macOS in tmux", env("darwin", map[string]string{"TMUX": "/tmp/t"}, "pbcopy", "tmux"), []string{"pbcopy", "tmux"}, false},
		{"Wayland", env("linux", map[string]string{"WAYLAND_DISPLAY": "w", "DISPLAY": ":0"}, "wl-copy", "xclip"), []string{"wl-copy"}, false},
		{"X with xsel alone", env("linux", map[string]string{"DISPLAY": ":0"}, "xsel"), []string{"xsel"}, false},
		{"WSL", env("linux", nil, "clip.exe"), []string{"clip.exe"}, false},
		{"Linux with nothing", env("linux", map[string]string{"DISPLAY": ":0"}), nil, false},
		// Over SSH a tool on the server fills the server's clipboard: OSC 52
		// asks the user's terminal instead, and tmux's buffer still helps.
		{"SSH", env("linux", map[string]string{"SSH_CONNECTION": "x", "DISPLAY": ":0"}, "xclip"), nil, true},
		{"SSH in tmux", env("darwin", map[string]string{"SSH_TTY": "/dev/ttys1", "TMUX": "t"}, "pbcopy", "tmux"), []string{"tmux"}, true},
		{"tmux not on PATH", env("darwin", map[string]string{"TMUX": "t"}, "pbcopy"), []string{"pbcopy"}, false},
	} {
		tools, osc52 := Tools(tc.env)
		if !slices.Equal(names(tools), tc.want) || osc52 != tc.osc52 {
			t.Errorf("%s: %q, osc52 %v; want %q, %v", tc.name, names(tools), osc52, tc.want, tc.osc52)
		}
	}
}

// TestRun copies with programs that stand in for the real tools: one that
// keeps the text, one that fails, and one that forks a child holding its
// output, as xclip does, which must not keep Run waiting.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	got := filepath.Join(dir, "copied")
	tools := []Tool{
		{"keeps", []string{"sh", "-c", "cat > " + got}},
		{"fails", []string{"sh", "-c", "exit 3"}},
		{"forks", []string{"sh", "-c", "cat >/dev/null; (sleep 5) &"}},
	}
	start := time.Now()
	copied, errs := Run(context.Background(), tools, "/home/me/x.go")
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v: a tool's child kept Run waiting", took)
	}
	if !slices.Equal(copied, []string{"keeps", "forks"}) || len(errs) != 1 {
		t.Errorf("copied %q, errors %v", copied, errs)
	}
	if b, err := os.ReadFile(got); err != nil || string(b) != "/home/me/x.go" {
		t.Errorf("the tool got %q, %v", b, err)
	}
}
