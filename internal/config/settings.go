package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// setting is one "name = value" line fndr understands. The table below is
// the one place a setting is named and given its default: Default, Parse
// and the file fndr writes for a new user all read it, so they cannot
// disagree.
type setting struct {
	name string
	def  string // the default, as it would be written in the file
	set  func(c *Config, value string) error
}

var settings = []setting{
	{"icons", "on", onOff(func(c *Config) *bool { return &c.Icons })},
	{"preview", "off", onOff(func(c *Config) *bool { return &c.Preview })},
	{"images", "on", onOff(func(c *Config) *bool { return &c.Images })},
	{"history", "on", onOff(func(c *Config) *bool { return &c.History })},
	{"save_index", "on", onOff(func(c *Config) *bool { return &c.SaveIndex })},
	{"editor", "", func(c *Config, v string) error { c.Editor = v; return nil }},
	{"editor_gui", "off", onOff(func(c *Config) *bool { return &c.EditorGUI })},
}

// onOff returns the setter of a switch. "true" and "false" are taken too,
// as Ghostty writes them: whoever comes from there should not be told off
// for it.
func onOff(field func(*Config) *bool) func(*Config, string) error {
	return func(c *Config, v string) error {
		switch strings.ToLower(v) {
		case "on", "true":
			*field(c) = true
		case "off", "false":
			*field(c) = false
		default:
			return fmt.Errorf("%q: want on or off", v)
		}
		return nil
	}
}

// Action is something the keys can do while searching. Its keys are set by
// one line, "key.<name> = <keys>": every key that does it, so changing the
// line is all it takes to move it, and the old key is free again.
type Action struct {
	Name string
	Keys []string // the default, as Keystroke names them

	// Typing: its keys may be ones that type, as "-": it acts only while
	// nothing is typed, and types otherwise. Any other action given such
	// a key would take a letter away from the query.
	Typing bool
}

// Actions are fndr's own actions, in the order the file lists them.
var Actions = []Action{
	{Name: "down", Keys: []string{"ctrl+n", "down"}},
	{Name: "up", Keys: []string{"ctrl+p", "up"}},
	{Name: "page_down", Keys: []string{"pgdown"}},
	{Name: "page_up", Keys: []string{"pgup"}},
	{Name: "first", Keys: []string{"home"}},
	{Name: "last", Keys: []string{"end"}},

	{Name: "open", Keys: []string{"enter"}},
	{Name: "parent", Keys: []string{"-"}, Typing: true},
	{Name: "back", Keys: []string{"esc"}},
	{Name: "quit", Keys: []string{"ctrl+c"}},
	{Name: "jump", Keys: []string{"ctrl+e"}},
	{Name: "global", Keys: []string{"tab"}},
	{Name: "tag", Keys: []string{"ctrl+t"}},
	{Name: "clear", Keys: []string{"ctrl+u"}},
	{Name: "oil", Keys: []string{"ctrl+o"}},
	{Name: "rename", Keys: []string{"ctrl+r"}},
	{Name: "copy_path", Keys: []string{"ctrl+y"}},
	{Name: "reveal", Keys: []string{"ctrl+g"}},
	{Name: "preview", Keys: []string{"ctrl+v"}},
	{Name: "preview_down", Keys: []string{"ctrl+j"}},
	{Name: "preview_up", Keys: []string{"ctrl+k"}},
	{Name: "help", Keys: []string{"f1"}},

	{Name: "cursor_left", Keys: []string{"ctrl+h", "left"}},
	{Name: "cursor_right", Keys: []string{"ctrl+l", "right"}},
	{Name: "delete_word", Keys: []string{"ctrl+w"}},
}

// keyPrefix starts the line of an action's keys. Without it the action
// preview (ctrl+v) and the setting preview (on at start) would be one name.
const keyPrefix = "key."

// Default returns the settings of a file that sets nothing.
func Default() Config {
	var c Config
	for _, s := range settings {
		if err := s.set(&c, s.def); err != nil {
			panic("config: default of " + s.name + ": " + err.Error()) // the table is wrong: a test sees it
		}
	}
	c.Keys = make(map[string][]string, len(Actions))
	for _, a := range Actions {
		c.Keys[a.Name] = slices.Clone(a.Keys) // the table is never written through the map
	}
	return c
}

// parseKeys reads the keys of a "key.<name>" line: keystrokes apart by
// spaces, or "none", for an action no key does.
func parseKeys(a *Action, value string) ([]string, error) {
	if value == "none" {
		return []string{}, nil
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil, errors.New(`no keys: write them apart by spaces, or "none" to turn it off`)
	}
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		k, err := Keystroke(f)
		if err != nil {
			return nil, err
		}
		if slices.Contains(keys, k) {
			return nil, fmt.Errorf("%s twice", k)
		}
		if k == "backspace" {
			return nil, errors.New("backspace erases what is typed: it is the line's own")
		}
		if types(k) && !a.Typing {
			return nil, fmt.Errorf("%s types: %s%s needs a key with ctrl or alt, or it could not be typed in a query", k, keyPrefix, a.Name)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// settle gives each key to one action. A key the file wrote for an action
// is taken from wherever it was, a default or a line above: what was
// written last is what the user wants now. The action it is taken from
// loses it, which is said on the line that took it; otherwise that action
// would quietly stop working.
func (c *Config) settle(setOn map[string]int) []error {
	// Defaults first, then the file's lines in order: later ones take.
	order := make([]string, 0, len(Actions))
	for _, a := range Actions {
		if setOn[a.Name] == 0 {
			order = append(order, a.Name)
		}
	}
	written := make([]string, 0, len(setOn))
	for name := range setOn {
		written = append(written, name)
	}
	slices.SortFunc(written, func(a, b string) int { return setOn[a] - setOn[b] })
	order = append(order, written...)

	var errs []error
	owner := map[string]string{}
	for _, name := range order {
		for _, k := range c.Keys[name] {
			if prev, ok := owner[k]; ok {
				c.Keys[prev] = slices.DeleteFunc(c.Keys[prev], func(p string) bool { return p == k })
				errs = append(errs, &Error{setOn[name], fmt.Errorf("%s was %s%s's too; now only %s%s's", k, keyPrefix, prev, keyPrefix, name)})
			}
			owner[k] = name
		}
	}
	return errs
}

// types reports whether the key k types a character into the query: one
// character, shifted or not, or space. With any other modifier, ctrl+x,
// what is left is longer than one character.
func types(k string) bool {
	rest := strings.TrimPrefix(k, "shift+")
	return rest == "space" || utf8.RuneCountInString(rest) == 1
}
