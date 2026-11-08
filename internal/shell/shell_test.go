package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	if _, err := Init("zsh"); err != nil {
		t.Errorf("Init(zsh): %v", err)
	}
	if _, err := Init("cmd.exe"); err == nil {
		t.Error("Init(cmd.exe) succeeded, want an unsupported-shell error")
	}
}

// TestZshChangesDirectory runs the real function in a real zsh, with a fake
// fndr on PATH that writes a folder to its -cwd-file the way fndr does on
// Esc, and checks that the shell ends up there. Skipped where zsh is not
// installed (it is on every Mac, not on every CI runner).
func TestZshChangesDirectory(t *testing.T) {
	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target dir") // a space, to catch missing quotes
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(tmp, "bin")
	if err := os.Mkdir(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	// The fake fndr behaves like fndr quit with Esc: it writes $TARGET to
	// the file named after --cwd-file.
	fake := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = --cwd-file ]; then printf %s \"$TARGET\" > \"$2\"; fi\n  shift\ndone\n"
	if err := os.WriteFile(filepath.Join(bin, "fndr"), []byte(fake), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}

	script := zsh + "\nfndr\npwd\n"
	cmd := exec.Command(zshPath, "-f", "-c", script)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TARGET="+target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	want, _ := filepath.EvalSymlinks(target)
	if got != want {
		t.Errorf("after fndr the shell is in %q, want %q", got, want)
	}
}
