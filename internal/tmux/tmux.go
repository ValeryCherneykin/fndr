// Package tmux opens a tmux session for a folder, as a project switcher
// does: one session a folder, named after it, made the first time with a
// command of the user's in it (their editor), and switched to after.
//
// The package only talks to tmux through a Runner, so tests drive it with
// a fake, or with a server of their own (tmux -L), and never touch the
// user's sessions.
package tmux

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner runs tmux with args and returns what it printed. An error is a
// tmux that said no (a session not found, say), or no tmux at all.
type Runner func(args ...string) (string, error)

// ErrNoTmux is a machine without tmux.
var ErrNoTmux = errors.New("tmux is not installed")

// Exec is the Runner of the real tmux. Inside a session it is that
// session's server, by $TMUX; outside, the default one.
func Exec(args ...string) (string, error) {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return "", ErrNoTmux
	}
	out, err := exec.Command(path, args...).CombinedOutput() //nolint:gosec // tmux, with fndr's own arguments
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("tmux %s: %s", args[0], firstLine(text))
		}
		return "", fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return text, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// Name is the session name for dir: its own name, as the user knows the
// project, with "." and ":" made "_" (tmux reads them in a target as a
// window and a pane: "fndr.nvim" would name window "nvim").
func Name(dir string) string {
	name := strings.Map(func(r rune) rune {
		if r == '.' || r == ':' {
			return '_'
		}
		return r
	}, filepath.Base(dir))
	if name == "" || name == "_" || name == string(filepath.Separator) {
		name = "root"
	}
	return name
}

// hashed is the name of dir when Name is another folder's session already
// (~/dev/api and ~/work/api): the name and four hex digits of the path,
// the same each time, so the second api finds its own session again.
func hashed(dir string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(dir)) // a hash never fails to write
	return fmt.Sprintf("%s-%04x", Name(dir), h.Sum32()&0xffff)
}

// target names the session exactly: tmux takes "-t api" as any session
// starting so, "api-v2" too, if there is no api.
func target(name string) string { return "=" + name }

// Open makes sure dir has a session and returns its name. A new session
// starts in dir, and run, if not "", is typed in it, as at a prompt: when
// the editor quits, the shell is still there.
func Open(r Runner, dir, run string) (string, error) {
	// All sessions and their folders, in one call. Not display-message
	// per name: for a session that is not there it prints nothing and
	// says nothing wrong, as for one with no folder.
	out, err := r("list-sessions", "-F", "#{session_name}\t#{session_path}")
	if errors.Is(err, ErrNoTmux) {
		return "", err
	}
	sessions := map[string]string{}
	if err == nil { // an error is no server yet: no sessions
		for line := range strings.SplitSeq(out, "\n") {
			if name, path, ok := strings.Cut(line, "\t"); ok {
				sessions[name] = path
			}
		}
	}
	for _, name := range []string{Name(dir), hashed(dir)} {
		path, taken := sessions[name]
		switch {
		case !taken:
			if _, err := r("new-session", "-d", "-s", name, "-c", dir); err != nil {
				return "", err
			}
			if run != "" {
				if _, err := r("send-keys", "-t", target(name)+":", run, "Enter"); err != nil {
					return "", err
				}
			}
			return name, nil
		case filepath.Clean(path) == dir:
			return name, nil // there already
		}
		// Another folder's: try the name with the path's hash.
	}
	return "", fmt.Errorf("tmux: the sessions %q and %q are other folders'", Name(dir), hashed(dir))
}

// Switch moves the tmux client fndr runs in to the session name.
func Switch(r Runner, name string) error {
	_, err := r("switch-client", "-t", target(name))
	return err
}

// Attach is the command that attaches the terminal to the session name,
// from outside tmux: for fndr to become, once it has quit.
func Attach(name string) []string {
	return []string{"tmux", "attach-session", "-t", target(name)}
}
