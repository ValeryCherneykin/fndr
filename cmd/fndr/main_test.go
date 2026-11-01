package main

import (
	"bytes"
	"strings"
	"testing"
)

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
		{name: "no args", args: nil, wantCode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

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
