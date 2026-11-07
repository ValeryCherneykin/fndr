package opener

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// ErrNoEditor means no editor is configured and none of the usual ones is
// installed. The UI should say so rather than do nothing.
var ErrNoEditor = errors.New("no editor found: set editor in the config, or $VISUAL or $EDITOR")

// fallbackEditors are tried, in order, when nothing is configured. vi is
// required by POSIX, so this almost never comes up empty.
var fallbackEditors = []string{"nvim", "vim", "vi"}

// env is the slice of the operating system the editor choice depends on.
// Passed in rather than read directly so tests do not depend on the machine
// they run on.
type env struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
}

var osEnv = env{getenv: os.Getenv, lookPath: exec.LookPath}

// Editor returns the command line of the editor to open text files with:
// configured if set, else $VISUAL, else $EDITOR, else the first of
// nvim, vim and vi found in $PATH. The result is a program and its
// arguments, such as ["code", "-w"]; the file path goes after them.
func Editor(configured string) ([]string, error) {
	return osEnv.editor(configured)
}

// editor is Editor with the environment injected. A value that is empty or
// only whitespace counts as unset.
func (e env) editor(configured string) ([]string, error) {
	for _, cmd := range []string{configured, e.getenv("VISUAL"), e.getenv("EDITOR")} {
		if fields := strings.Fields(cmd); len(fields) > 0 {
			return fields, nil
		}
	}
	for _, name := range fallbackEditors {
		if _, err := e.lookPath(name); err == nil {
			return []string{name}, nil
		}
	}
	return nil, ErrNoEditor
}
