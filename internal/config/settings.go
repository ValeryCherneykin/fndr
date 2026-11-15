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
	name  string
	def   string // the default, as it would be written in the file
	set   func(c *Config, value string) error
	doc   string // what it does, for the file: a line or two
	group string // a heading, on the first setting of a group
}

var settings = []setting{
	{name: "icons", def: "on", set: onOff(func(c *Config) *bool { return &c.Icons }), group: "Look",
		doc: "Nerd Font icons before names. off: plain names, and a \"/\" after a folder,\nfor a terminal without a Nerd Font."},
	{name: "preview", def: "off", set: onOff(func(c *Config) *bool { return &c.Preview }),
		doc: "The preview pane is open at start. ctrl+v opens and closes it anyway."},
	{name: "images", def: "on", set: onOff(func(c *Config) *bool { return &c.Images }),
		doc: "Pictures in the preview, where the terminal can show them (Ghostty, kitty).\noff: a card with the size and date, as for any file; lighter over SSH."},

	{name: "history", def: "on", set: onOff(func(c *Config) *bool { return &c.History }), group: "Memory",
		doc: "Learn where you go, to put it first. Kept in ~/.local/state/fndr.\noff: nothing is read or written; delete the file yourself if you like."},
	{name: "save_index", def: "on", set: onOff(func(c *Config) *bool { return &c.SaveIndex }),
		doc: "Keep the index of your home between runs, for an instant start.\nKept in ~/.cache/fndr: the names of your files. off: every start reads\nthe disk again; delete the file yourself if you like."},

	{name: "editor", def: "", set: func(c *Config, v string) error { c.Editor = v; return nil }, group: "Opening files",
		doc: "The editor for text files, with its flags: nvim, hx, code.\nEmpty: $VISUAL, then $EDITOR, then nvim, vim or vi."},
	{name: "editor_gui", def: "off", set: onOff(func(c *Config) *bool { return &c.EditorGUI }),
		doc: "on: the editor above opens a window of its own (VS Code, PyCharm),\nand fndr goes on instead of waiting for it. Write no --wait flag."},
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
	Doc  string   // what it does, one line: for the file and the help page

	// Typing: its keys may be ones that type, as "-": it acts only while
	// nothing is typed, and types otherwise. Any other action given such
	// a key would take a letter away from the query.
	Typing bool

	Group string // a heading, on the first action of a group
}

// Actions are fndr's own actions, in the order the file lists them.
var Actions = []Action{
	{Name: "down", Keys: []string{"ctrl+n", "down"}, Doc: "Move down the list.", Group: "Moving"},
	{Name: "up", Keys: []string{"ctrl+p", "up"}, Doc: "Move up the list."},
	{Name: "page_down", Keys: []string{"pgdown"}, Doc: "A page down."},
	{Name: "page_up", Keys: []string{"pgup"}, Doc: "A page up."},
	{Name: "first", Keys: []string{"home"}, Doc: "To the first row."},
	{Name: "last", Keys: []string{"end"}, Doc: "To the last row."},

	{Name: "open", Keys: []string{"enter"}, Doc: "Open: a folder goes in, text goes to the editor, the rest to its app.", Group: "Doing"},
	{Name: "parent", Keys: []string{"-"}, Typing: true, Doc: "Up one folder, while nothing is typed; in a query the key types."},
	{Name: "back", Keys: []string{"esc"}, Doc: "One step back: the last tag, the query, GLOBAL, then quit."},
	{Name: "quit", Keys: []string{"ctrl+c"}, Doc: "Quit; the shell stays where it was."},
	{Name: "jump", Keys: []string{"ctrl+e"}, Doc: "Quit, and the shell goes to the selected folder (needs fndr init)."},
	{Name: "global", Keys: []string{"tab"}, Doc: "Search this folder, or the whole home."},
	{Name: "ignored", Keys: []string{"ctrl+d"}, Doc: "Search what .gitignore and node_modules leave out too: build/, dist/."},
	{Name: "tag", Keys: []string{"ctrl+t"}, Doc: "Make what is typed a tag: .go, dirs, >1mb, @today, has:.json."},
	{Name: "clear", Keys: []string{"ctrl+u"}, Doc: "Erase the query before the cursor; with none, take all tags off."},
	{Name: "oil", Keys: []string{"ctrl+o"}, Doc: "Edit the folder, or what was found, as text with Vim keys."},
	{Name: "rename", Keys: []string{"ctrl+r"}, Doc: "Rename or move the selected entry by editing its path."},
	{Name: "copy_path", Keys: []string{"ctrl+y"}, Doc: "Copy, then y the full path, r the path from here, n the name."},
	{Name: "reveal", Keys: []string{"ctrl+g"}, Doc: "Show the selected entry in Finder, or the file manager."},
	{Name: "preview", Keys: []string{"ctrl+v"}, Doc: "Open or close the preview pane."},
	{Name: "preview_down", Keys: []string{"ctrl+j"}, Doc: "Scroll the preview down."},
	{Name: "preview_up", Keys: []string{"ctrl+k"}, Doc: "Scroll the preview up."},
	{Name: "help", Keys: []string{"f1"}, Doc: "The help page."},

	{Name: "cursor_left", Keys: []string{"ctrl+h", "left"}, Doc: "Move the cursor in the query left.", Group: "Typing"},
	{Name: "cursor_right", Keys: []string{"ctrl+l", "right"}, Doc: "Move the cursor in the query right."},
	{Name: "delete_word", Keys: []string{"ctrl+w"}, Doc: "Erase the word before the cursor."},
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
