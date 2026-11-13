package opener

import (
	"errors"
	"slices"
	"testing"
)

func TestSystem(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		vars      map[string]string
		installed []string
		want      []string
	}{
		{"macOS", "darwin", nil, nil, []string{"open", "/a b.pdf"}},
		{"linux with xdg-open", "linux", nil, []string{"xdg-open", "gio"}, []string{"xdg-open", "/a b.pdf"}},
		{"linux with gio only", "linux", nil, []string{"gio"}, []string{"gio", "open", "/a b.pdf"}},
		{
			"WSL", "linux",
			map[string]string{"WSL_DISTRO_NAME": "Ubuntu"},
			[]string{"wslview", "xdg-open"},
			[]string{"wslview", "/a b.pdf"},
		},
		{
			"WSL without wslview falls back", "linux",
			map[string]string{"WSL_DISTRO_NAME": "Ubuntu"},
			[]string{"xdg-open"},
			[]string{"xdg-open", "/a b.pdf"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fakeEnv(tt.vars, tt.installed...).system(tt.goos, "/a b.pdf")
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("system() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func TestSystemNoneFound(t *testing.T) {
	for _, goos := range []string{"linux", "plan9"} {
		if _, err := fakeEnv(nil).system(goos, "/a.pdf"); !errors.Is(err, ErrNoOpener) {
			t.Errorf("%s: err = %v, want ErrNoOpener", goos, err)
		}
	}
}

// TestSystemDoesNotShareOpeners guards against append writing into the
// shared linuxOpeners table: a command line built for one file must not
// show up in the next.
func TestSystemDoesNotShareOpeners(t *testing.T) {
	e := fakeEnv(nil, "gio")
	first, _ := e.system("linux", "/first")
	_, _ = e.system("linux", "/second")
	if want := []string{"gio", "open", "/first"}; !slices.Equal(first, want) {
		t.Errorf("first command became %q, want %q", first, want)
	}
}

func TestReveal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		goos  string
		path  string
		isDir bool
		want  []string
	}{
		{"macOS folder", "darwin", "/u/dev", true, []string{"open", "/u/dev"}},
		{"macOS file: selected", "darwin", "/u/dev/a.go", false, []string{"open", "-R", "/u/dev/a.go"}},
		// open Ghostty.app starts Ghostty: a folder with an extension is
		// shown, never opened.
		{"macOS app: shown, not run", "darwin", "/Applications/Ghostty.app", true, []string{"open", "-R", "/Applications/Ghostty.app"}},
		{"macOS library: shown", "darwin", "/u/Pictures/Photos Library.photoslibrary", true, []string{"open", "-R", "/u/Pictures/Photos Library.photoslibrary"}},
		{"linux folder", "linux", "/u/dev", true, []string{"xdg-open", "/u/dev"}},
		{"linux file: its folder", "linux", "/u/dev/a.go", false, []string{"xdg-open", "/u/dev"}},
	} {
		got, err := fakeEnv(nil, "xdg-open").reveal(tc.goos, tc.path, tc.isDir)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := fakeEnv(nil).reveal("linux", "/u/a.go", false); !errors.Is(err, ErrNoOpener) {
		t.Errorf("linux with nothing: %v, want ErrNoOpener", err)
	}
}

func TestIsApp(t *testing.T) {
	for _, tc := range []struct {
		goos, path string
		isDir      bool
		want       bool
	}{
		{"darwin", "/Applications/Ghostty.app", true, true},
		{"darwin", "/Applications/Some.APP", true, true},
		{"darwin", "/u/dev/x.app", false, false}, // a file named so
		{"darwin", "/u/Pictures/Photos Library.photoslibrary", true, false},
		{"darwin", "/u/dev/fndr/bin/fndr", false, false},
		{"linux", "/opt/x.app", true, false},
	} {
		if got := isApp(tc.goos, tc.path, tc.isDir); got != tc.want {
			t.Errorf("%s %s dir=%v: %v, want %v", tc.goos, tc.path, tc.isDir, got, tc.want)
		}
	}
}
