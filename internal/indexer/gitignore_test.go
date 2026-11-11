package indexer

import (
	"strings"
	"testing"
	"time"
)

// TestGitignoreMatchesFolders: a .gitignore's patterns, matched against
// folders below it, the way git reads them.
func TestGitignoreMatchesFolders(t *testing.T) {
	tests := []struct {
		file string // the .gitignore
		dir  string // a folder below it, relative
		want bool
	}{
		{"bin", "bin", true},
		{"bin", "cmd/bin", true}, // no slash: a name at any depth
		{"bin/", "bin", true},    // a trailing slash: folders only, and these are
		{"/bin", "bin", true},    // a leading slash: here only
		{"/bin", "cmd/bin", false},
		{"cmd/bin", "cmd/bin", true}, // a slash inside anchors it too
		{"cmd/bin", "x/cmd/bin", false},
		{"*.egg-info", "pkg/a.egg-info", true},
		{"build?", "build2", true},
		{"[bt]arget", "target", true},
		{"**/gen", "a/b/gen", true},
		{"**/gen", "gen", true},
		{"docs/**/out", "docs/out", true},
		{"docs/**/out", "docs/a/b/out", true},
		{"docs/**/out", "x/docs/out", false},
		{"out/**", "out/sub", true},
		{"out/**", "out", false},
		{"**/**/gen", "gen", true}, // twice in a row is once
		{"a/**/**/b", "a/x/y/b", true},
		{"a/**/**", "a", false},
		{"**/a/**/b", "x/a/y/z/b", true},
		{"**/a/**/b", "x/b/y/a", false},
		{"a/b/c/d", "a/b", false},      // more names than folders
		{"dist\n!dist", "dist", false}, // the last match wins
		{"*\n!keep", "keep", false},
		{"# bin", "bin", false},  // a comment
		{"\\#bin", "#bin", true}, // an escaped hash is a name
		{"\\!bin", "!bin", true}, // and so is an escaped bang
		{"bin   ", "bin", true},  // trailing spaces are dropped
		{"", "bin", false},
		{"[", "[", false}, // a broken pattern matches nothing
		{"bin", "binary", false},
	}
	for _, tt := range tests {
		g := parseGitignore("/r", []byte(tt.file))
		_, got := g.match(strings.Split(tt.dir, "/"))
		if got != tt.want {
			t.Errorf("%q against folder %q: %v, want %v", tt.file, tt.dir, got, tt.want)
		}
	}
}

// TestGitignoreStarsStayFast: a .gitignore comes with whatever repository
// was cloned, so a hostile one must not stall the crawl. Many "**" against
// a deep folder used to take minutes for that one folder; tried every way
// to split the path, over and over.
func TestGitignoreStarsStayFast(t *testing.T) {
	deep := strings.Split(strings.Repeat("a/", 40)+"b", "/")
	for _, file := range []string{
		strings.Repeat("**/", 1000) + "z", // in a row
		strings.Repeat("**/a/", 20) + "z", // apart, every "a" a fit
		strings.Repeat("a/**/", 20) + "b", // and one that matches
	} {
		g := parseGitignore("/r", []byte(file))
		done := make(chan struct{})
		go func() {
			g.match(deep)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%.20q… still matching one folder after 5s", file)
		}
	}
}

// TestGitignoreStack: a deeper .gitignore has the last word over the one
// above it, and each reads paths from its own folder.
func TestGitignoreStack(t *testing.T) {
	var s ignoreStack
	s = s.with(parseGitignore("/r", []byte("gen\n/top")))
	s = s.with(parseGitignore("/r/app", []byte("!gen\nlocal")))
	for path, want := range map[string]bool{
		"/r/gen":       true,
		"/r/app/gen":   false, // re-included below
		"/r/top":       true,
		"/r/app/top":   false, // /top is /r/top only
		"/r/app/local": true,
		"/r/local":     false, // app's rules stop at app
	} {
		if got := s.ignores(path); got != want {
			t.Errorf("%s: ignored %v, want %v", path, got, want)
		}
	}
}
