package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Template returns the settings file fndr writes for a new user, and prints
// for `fndr config`: every setting and every action, at its default,
// written out, each with a line on what it does. Whoever wants pictures
// off finds "images = on" and writes off; nothing is to be learned first.
//
// The values are written out, not commented out: changing one is all it
// takes. The price is that the file keeps the defaults of the fndr that
// wrote it; `fndr config` prints the file a newer one would write.
func Template() string {
	var b strings.Builder
	b.WriteString(`# fndr settings. Change a value, save, and start fndr again.
# A line deleted is its default. "fndr config" prints this file as the
# fndr installed now would write it, with every setting it knows.
#
# Comments go on a line of their own, as here: "images = off # x" is a
# mistake, which fndr names, with its line, when it starts.
`)
	for _, s := range settings {
		heading(&b, s.group)
		comment(&b, s.doc)
		b.WriteString(strings.TrimRight(s.name+" = "+s.def, " ") + "\n")
	}

	heading(&b, "Folders")
	b.WriteString(`# fndr lists, but does not go into: hidden folders but ~/.config,
# node_modules and caches, ~/Library, the Go package store, and what
# .gitignore files name. A line a folder, as many as you like.
#
# skip: one more folder to list but not go into.
# skip = ~/Movies
#
# include: go into one of those all the same. Its names land in the saved
# index too (~/.cache/fndr, only yours to read). The folder holding it
# must be gone into: for ~/.local/share/x, include ~/.local first.
# include = ~/.ssh
`)

	heading(&b, "Keys while searching")
	b.WriteString(`# One line per action: every key that does it, apart by spaces. Change
# the line and the key it had is free again; "none" turns the action off.
# A key is ctrl+x, alt+x, shift+x, f1 to f24, or one of enter tab esc
# space backspace up down left right home end pgup pgdown insert delete.
# A key that types, as j, is for typing, and backspace erases.
# alt on macOS needs Option to be Alt in the terminal: in Ghostty,
# macos-option-as-alt = left; otherwise alt+x types a letter.
# Inside OIL the keys are Vim's; esc, q and ctrl+o go back.
`)
	for _, a := range Actions {
		if a.Group != "" {
			b.WriteString("\n# " + a.Group + "\n")
		}
		comment(&b, a.Doc)
		b.WriteString(keyPrefix + a.Name + " = " + strings.Join(a.Keys, " ") + "\n")
	}

	heading(&b, "Your tags")
	b.WriteString(`# A chord turns a tag on or off, as typing it and ctrl+t would: press
# ctrl+f, then f. The keys after the first are any; the first needs ctrl,
# alt or an f key. Take the # off to use these.
# bind = ctrl+f>f = tag:dirs
# bind = ctrl+f>t = tag:@today
# bind = ctrl+f>g = tag:.go
`)
	return b.String()
}

func heading(b *strings.Builder, title string) {
	if title != "" {
		b.WriteString("\n\n# --- " + title + " ---\n")
	}
}

// comment writes text as comment lines, then the line's own blank above
// it, so every setting stands apart.
func comment(b *strings.Builder, text string) {
	b.WriteString("\n")
	for line := range strings.SplitSeq(text, "\n") {
		b.WriteString("# " + line + "\n")
	}
}

// WriteNew writes the Template at path, making its folder, if there is no
// file there: a file the user has is never touched, whatever it holds. It
// reports whether it wrote one.
func WriteNew(path string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // ~/.config/fndr: the user's own, readable as ~/.config is
		return false, err
	}
	// O_EXCL: the check and the creating are one step, so a file made
	// between them, by another fndr in a tmux pane, is not written over.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // a settings file, readable as dotfiles are
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(Template()); err != nil {
		_ = f.Close()
		_ = os.Remove(path) // half a file would be read as the user's
		return false, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return false, err
	}
	return true, nil
}
