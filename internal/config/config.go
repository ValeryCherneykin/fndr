// Package config reads fndr's settings file, written the way Ghostty's
// is: one "key = value" a line, "#" at the start of a line for a comment,
// and a key may come again (bind, once per key it binds).
//
//	# ~/.config/fndr/config
//	bind = ctrl+f>f = tag:dirs
//	bind = ctrl+f>t = tag:@today
//
// A line that is wrong is reported with its number and skipped; the lines
// around it still count. A typo must not cost the rest of the file, nor
// stop fndr from starting.
//
// The package knows the file's grammar, not what fndr does with it: that
// a tag parses, or that a key is fndr's own, is for the interface to say.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Config is what the file sets.
type Config struct {
	Binds []Bind
}

// Bind is one "bind = keys = action:arg" line.
type Bind struct {
	// Keys are the keystrokes to press, in order: ctrl+f>f is "ctrl+f",
	// then "f". Each is written as the interface names a key press
	// (Bubble Tea's Keystroke): modifiers in the order ctrl, alt, shift,
	// meta, hyper, super, then the key in lower case.
	Keys []string

	// Action is what the keys do; for now only "tag", which turns the tag
	// in Arg on or off, as typing it and ctrl+t would.
	Action string
	Arg    string

	Line int // where in the file, for messages about it
}

// Error is a line of the file that could not be used.
type Error struct {
	Line int
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }

func (e *Error) Unwrap() error { return e.Err }

// DefaultPath returns where the file is looked for: fndr/config in
// $XDG_CONFIG_HOME, or in ~/.config, also on macOS, as Ghostty does: a
// terminal user's settings live in ~/.config, not in ~/Library.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "fndr", "config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fndr", "config"), nil
}

// Load reads the file at path. No file is no settings, not an error: most
// people never write one. Anything else that stops the file being read is
// returned alone.
func Load(path string) (Config, []error) {
	f, err := os.Open(path) //nolint:gosec // the user's own settings file, by design
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, []error{err}
	}
	defer f.Close() //nolint:errcheck // read only: closing cannot lose anything
	return Parse(f)
}

// Parse reads settings from r: everything it could, and an *Error for
// each line it could not.
func Parse(r io.Reader) (Config, []error) {
	var (
		c    Config
		errs []error
	)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case !ok:
			errs = append(errs, &Error{n, errors.New(`want "key = value"`)})
		case key == "bind":
			b, err := parseBind(value)
			if err != nil {
				errs = append(errs, &Error{n, err})
				continue
			}
			b.Line = n
			c.Binds = append(c.Binds, b)
		default:
			errs = append(errs, &Error{n, fmt.Errorf("unknown setting %q", key)})
		}
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err) // a line longer than 64 KB: not a settings file
	}
	return c, errs
}

// parseBind reads what follows "bind =": keys = action:arg.
func parseBind(s string) (Bind, error) {
	keys, action, ok := strings.Cut(s, "=")
	if !ok {
		return Bind{}, errors.New(`want "bind = keys = action", as bind = ctrl+f>f = tag:dirs`)
	}
	var b Bind
	for k := range strings.SplitSeq(keys, ">") {
		ks, err := Keystroke(k)
		if err != nil {
			return Bind{}, err
		}
		b.Keys = append(b.Keys, ks)
	}
	name, arg, _ := strings.Cut(strings.TrimSpace(action), ":")
	switch name {
	case "tag":
		if strings.TrimSpace(arg) == "" {
			return Bind{}, errors.New("tag: needs a tag, as tag:.go")
		}
	default:
		return Bind{}, fmt.Errorf("unknown action %q; there is tag:<tag>", name)
	}
	b.Action, b.Arg = name, strings.TrimSpace(arg)
	return b, nil
}

// mods are the modifiers in the order Bubble Tea names them.
var mods = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

// named are the keys that have a name rather than a character.
var named = map[string]bool{
	"enter": true, "tab": true, "backspace": true, "esc": true, "space": true,
	"up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pgup": true, "pgdown": true,
	"insert": true, "delete": true,
}

// Keystroke returns the key written as s (Ctrl+F, alt+shift+x, f5) in the
// form the interface names key presses, so the two compare as strings.
func Keystroke(s string) (string, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "+")
	key := parts[len(parts)-1]
	has := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		if !slices.Contains(mods, p) {
			return "", fmt.Errorf("%q: %q is not a modifier (ctrl, alt, shift, meta, hyper, super)", s, p)
		}
		if has[p] {
			return "", fmt.Errorf("%q: %s twice", s, p)
		}
		has[p] = true
	}
	if !isKey(key) {
		return "", fmt.Errorf("%q: %q is not a key", s, key)
	}
	var b strings.Builder
	for _, m := range mods {
		if has[m] {
			b.WriteString(m)
			b.WriteByte('+')
		}
	}
	b.WriteString(key)
	return b.String(), nil
}

// isKey reports whether k names a key: one printable character, or a name.
func isKey(k string) bool {
	if named[k] {
		return true
	}
	if n, ok := strings.CutPrefix(k, "f"); ok && n != "" && len(n) <= 2 && strings.Trim(n, "0123456789") == "" {
		return n[0] != '0' && (len(n) == 1 || n <= "24" && len(n) == 2) // f1 to f24
	}
	r, size := utf8.DecodeRuneInString(k)
	return size == len(k) && r != utf8.RuneError && unicode.IsGraphic(r) && !unicode.IsSpace(r)
}
