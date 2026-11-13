package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	for _, sh := range []string{"zsh", "bash", "fish"} {
		if _, err := Init(sh); err != nil {
			t.Errorf("Init(%s): %v", sh, err)
		}
	}
	if _, err := Init("cmd.exe"); err == nil {
		t.Error("Init(cmd.exe) succeeded, want an unsupported-shell error")
	}
}

// TestChangesDirectory runs each shell's function in the real shell, with a
// fake fndr on PATH that writes a folder to its -cwd-file the way fndr does
// on Ctrl+E, and checks that the shell ends up there. A shell that is not
// installed is skipped (zsh is on every Mac, fish on few machines).
func TestChangesDirectory(t *testing.T) {
	for _, sh := range []struct {
		name string
		args []string // run the script given last, without the user's config
	}{
		{"zsh", []string{"-f", "-c"}},
		{"bash", []string{"--norc", "--noprofile", "-c"}},
		{"fish", []string{"--no-config", "-c"}},
	} {
		t.Run(sh.name, func(t *testing.T) {
			path, err := exec.LookPath(sh.name)
			if err != nil {
				t.Skip(sh.name, "not installed")
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
			// The fake fndr behaves like fndr quit with Ctrl+E: it writes
			// $TARGET to the file named after --cwd-file.
			fake := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = --cwd-file ]; then printf %s \"$TARGET\" > \"$2\"; fi\n  shift\ndone\n"
			if err := os.WriteFile(filepath.Join(bin, "fndr"), []byte(fake), 0o700); err != nil { //nolint:gosec // test helper must be executable
				t.Fatal(err)
			}
			script, err := Init(sh.name)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(path, append(sh.args, script+"\nfndr\npwd\n")...) //nolint:gosec // the test's own shells
			cmd.Dir = tmp
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TARGET="+target)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", sh.name, err, out)
			}
			got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
			want, _ := filepath.EvalSymlinks(target)
			if got != want {
				t.Errorf("after fndr the shell is in %q, want %q", got, want)
			}
		})
	}
}
