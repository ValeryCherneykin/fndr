package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// noUI stands in for the interface, so tests never take over the terminal.
func noUI(string) error { return nil }

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // checked as a prefix; empty means stdout must be empty
	}{
		{name: "version", args: []string{"-version"}, wantCode: 0, wantStdout: "fndr "},
		{name: "help is not an error", args: []string{"-h"}, wantCode: 0},
		{name: "unknown flag", args: []string{"-nope"}, wantCode: 2},
		{name: "no args opens the interface", args: nil, wantCode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr, noUI)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if tt.wantStdout == "" && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.HasPrefix(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want prefix %q", stdout.String(), tt.wantStdout)
			}
		})
	}
}

func TestResolveVersionPrefersLdflags(t *testing.T) {
	// Not parallel: the test mutates the package-level version variable.
	old := version
	t.Cleanup(func() { version = old })

	version = "v1.2.3"
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("resolveVersion() = %q, want %q", got, "v1.2.3")
	}
}

func TestRunOpensUIOnCurrentDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var got string
	ui := func(root string) error { got = root; return nil }
	if code := run(nil, &bytes.Buffer{}, &bytes.Buffer{}, ui); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// TempDir may sit behind a symlink (/var -> /private/var on macOS); the
	// interface must get the resolved path, which the crawler accepts.
	if want, _ := filepath.EvalSymlinks(dir); got != want {
		t.Errorf("ui root = %q, want %q", got, want)
	}
}

func TestRunReportsUIError(t *testing.T) {
	var stderr bytes.Buffer
	ui := func(string) error { return errors.New("no tty") }
	if code := run(nil, &bytes.Buffer{}, &stderr, ui); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no tty") {
		t.Errorf("stderr = %q, want the error", stderr.String())
	}
}
