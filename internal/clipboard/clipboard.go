// Package clipboard puts text on the system clipboard, by every way that
// fits where fndr runs, and says which worked: a copy that silently does
// nothing is worse than an error.
//
// The ways:
//
//   - the system's own tool: pbcopy on macOS; wl-copy on Wayland, xclip or
//     xsel on X; clip.exe in WSL. One is enough, the first found;
//   - tmux's buffer, inside tmux: what prefix ] pastes, and, with tmux's
//     set-clipboard, the terminal's clipboard too;
//   - OSC 52, over SSH: an escape sequence asking the terminal itself, on
//     the user's machine, to copy. A tool on the server would fill the
//     server's clipboard, which nobody sees. The interface sends it, as it
//     owns the terminal (Tools only says to).
//
// The package only decides and runs; the environment is passed in, so it
// is tested without touching the clipboard of whoever runs the tests.
package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tool is a program that copies the text it reads on stdin.
type Tool struct {
	Name string   // as the status bar says it: "pbcopy", "tmux"
	Argv []string // the program and its arguments
}

// Env is what decides the ways: the environment, the programs on PATH and
// the operating system, as os.Getenv, exec.LookPath and runtime.GOOS.
type Env struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	GOOS     string
}

// Tools returns the programs to copy with, and whether OSC 52 should be
// sent as well. None, and no OSC 52, means there is no way to copy here.
func Tools(e Env) (tools []Tool, osc52 bool) {
	remote := e.Getenv("SSH_CONNECTION") != "" || e.Getenv("SSH_TTY") != ""
	if !remote {
		for _, t := range native(e) {
			if _, err := e.LookPath(t.Argv[0]); err == nil {
				tools = append(tools, t)
				break
			}
		}
	}
	if e.Getenv("TMUX") != "" {
		if _, err := e.LookPath("tmux"); err == nil {
			// -w: also to the terminal's clipboard, if tmux's set-clipboard
			// lets it.
			tools = append(tools, Tool{"tmux", []string{"tmux", "load-buffer", "-w", "-"}})
		}
	}
	return tools, remote
}

// native lists the system's own tools that fit, the likeliest first.
func native(e Env) []Tool {
	var ts []Tool
	switch e.GOOS {
	case "darwin":
		ts = append(ts, Tool{"pbcopy", []string{"pbcopy"}})
	case "windows":
		ts = append(ts, Tool{"clip.exe", []string{"clip.exe"}})
	default:
		if e.Getenv("WAYLAND_DISPLAY") != "" {
			ts = append(ts, Tool{"wl-copy", []string{"wl-copy"}})
		}
		if e.Getenv("DISPLAY") != "" {
			ts = append(ts,
				Tool{"xclip", []string{"xclip", "-selection", "clipboard"}},
				Tool{"xsel", []string{"xsel", "--clipboard", "--input"}})
		}
		ts = append(ts, Tool{"clip.exe", []string{"clip.exe"}}) // WSL: Windows' own, on PATH
	}
	return ts
}

// timeout bounds a tool. xclip and wl-copy stay behind to serve the copy,
// but fork for it, so the program run returns at once; one that hangs
// (an X server gone) must not keep the status waiting.
const timeout = 3 * time.Second

// Run copies text with each tool and returns the names of those that did
// it, and an error for each that did not.
func Run(ctx context.Context, tools []Tool, text string) (copied []string, errs []error) {
	for _, t := range tools {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(ctx, t.Argv[0], t.Argv[1:]...) //nolint:gosec // the tools are ours, chosen above
		cmd.Stdin = strings.NewReader(text)
		// No pipes for its output: xclip and wl-copy leave a process behind
		// to serve the copy, which would hold a pipe open, and Wait would
		// wait for it, to the timeout. Unread, the output goes to the null
		// device, and Wait returns when the program itself does.
		err := cmd.Run()
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Name, err))
			continue
		}
		copied = append(copied, t.Name)
	}
	return copied, errs
}
