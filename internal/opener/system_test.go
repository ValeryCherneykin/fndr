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
