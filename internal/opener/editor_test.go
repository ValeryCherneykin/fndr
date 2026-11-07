package opener

import (
	"errors"
	"os/exec"
	"slices"
	"testing"
)

// fakeEnv is a machine with the given variables set and programs installed.
func fakeEnv(vars map[string]string, installed ...string) env {
	return env{
		getenv: func(k string) string { return vars[k] },
		lookPath: func(name string) (string, error) {
			if slices.Contains(installed, name) {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
	}
}

func TestEditor(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		vars       map[string]string
		installed  []string
		want       []string
	}{
		{
			name:       "config wins over everything",
			configured: "code -w",
			vars:       map[string]string{"VISUAL": "nvim", "EDITOR": "vim"},
			want:       []string{"code", "-w"},
		},
		{
			name: "VISUAL before EDITOR",
			vars: map[string]string{"VISUAL": "nvim", "EDITOR": "vim"},
			want: []string{"nvim"},
		},
		{
			name: "EDITOR when VISUAL is unset",
			vars: map[string]string{"EDITOR": "hx"},
			want: []string{"hx"},
		},
		{
			name: "arguments are kept",
			vars: map[string]string{"EDITOR": "emacs -nw"},
			want: []string{"emacs", "-nw"},
		},
		{
			name:      "whitespace counts as unset",
			vars:      map[string]string{"VISUAL": "  ", "EDITOR": ""},
			installed: []string{"vim"},
			want:      []string{"vim"},
		},
		{
			name:      "first installed fallback",
			installed: []string{"vi", "vim"},
			want:      []string{"vim"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fakeEnv(tt.vars, tt.installed...).editor(tt.configured)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("editor() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func TestEditorNoneFound(t *testing.T) {
	_, err := fakeEnv(nil).editor("")
	if !errors.Is(err, ErrNoEditor) {
		t.Errorf("err = %v, want ErrNoEditor", err)
	}
}
