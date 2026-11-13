package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/config"
	"github.com/ValeryCherneykin/fndr/internal/filter"
)

// Binds are the user's own keys, from the config file: a tag a chord away
// (bind = ctrl+f>f = tag:dirs). A chord of several keys waits after each
// but the last, and the status line says what may follow: the user sees
// the choices instead of remembering them. Esc gives up waiting.
//
// Keys are compared by Keystroke, the key and not the letter it types: on
// a Russian layout, ctrl+f>f is still ctrl+f, then the key that types а.

// binding is a bind the interface can act on.
type binding struct {
	keys []string
	tag  filter.Tag
}

// ownKeys are the keys fndr itself answers while searching. Binding one
// would take it away from the user unannounced; rebinding fndr's keys is
// for later (v0.7), with help that shows what they became. f1 is kept for
// that help.
var ownKeys = []string{
	"ctrl+c", "ctrl+e", "ctrl+h", "ctrl+l", "ctrl+n", "ctrl+o", "ctrl+p",
	"ctrl+r", "ctrl+t", "ctrl+u", "ctrl+v", "ctrl+w", "f1",
}

// useConfig takes the settings read from the config file, and says its
// mistakes at once, the file's (errs) and those only the interface can
// see: a bind that silently does nothing is worse than a line of red.
func (m *Model) useConfig(cfg config.Config, errs []error) {
	errs = append(errs, m.useBinds(cfg.Binds)...)
	if len(errs) == 0 {
		return
	}
	msg := "config " + errs[0].Error()
	if len(errs) > 1 {
		msg += fmt.Sprintf(" (and %d more)", len(errs)-1)
	}
	m.setNotice(msg, true)
}

// useBinds turns the config's binds into bindings, and says, in the status
// line, what it could not use and why. A bind that comes first wins over a
// later one that clashes with it, so adding a line never breaks another.
func (m *Model) useBinds(binds []config.Bind) []error {
	var errs []error
	for _, b := range binds {
		err := m.checkBind(b)
		if err == nil {
			var tag filter.Tag
			if tag, err = filter.Parse(b.Arg); err == nil {
				m.binds = append(m.binds, binding{keys: b.Keys, tag: tag})
				continue
			}
			err = fmt.Errorf("tag:%s: %w", b.Arg, err)
		}
		errs = append(errs, &config.Error{Line: b.Line, Err: err})
	}
	return errs
}

// checkBind says why b cannot be used, if it cannot.
func (m *Model) checkBind(b config.Bind) error {
	first := b.Keys[0]
	if !modified(first) && !isFKey(first) {
		return fmt.Errorf("%s would take a key away from typing: start with ctrl or alt", first)
	}
	if slices.Contains(ownKeys, first) {
		return fmt.Errorf("%s is fndr's own", first)
	}
	if slices.Contains(b.Keys[1:], "esc") {
		return fmt.Errorf("%s could never be pressed: esc in a chord gives it up", chord(b.Keys))
	}
	for _, o := range m.binds {
		n := min(len(o.keys), len(b.Keys))
		if slices.Equal(o.keys[:n], b.Keys[:n]) {
			return fmt.Errorf("%s clashes with %s, bound before", chord(b.Keys), chord(o.keys))
		}
	}
	return nil
}

// modified reports whether a keystroke is a chord with a modifier that
// typing never uses. shift alone types capitals.
func modified(k string) bool {
	for _, p := range []string{"ctrl+", "alt+", "meta+", "hyper+", "super+"} {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

func isFKey(k string) bool {
	n, ok := strings.CutPrefix(k, "f")
	return ok && n != "" && strings.Trim(n, "0123456789") == ""
}

func chord(keys []string) string { return strings.Join(keys, ">") }

// bound reports whether keys are a bind or the start of one.
func (m *Model) bound(keys []string) bool {
	for _, b := range m.binds {
		if len(b.keys) >= len(keys) && slices.Equal(b.keys[:len(keys)], keys) {
			return true
		}
	}
	return false
}

// bindKey handles k if it is part of a bind: the end of one does what it
// binds, the start of one waits for the rest. It reports whether k was
// taken; a key that is neither goes on as usual, unless a chord was
// waiting, which a wrong key ends.
func (m *Model) bindKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if len(m.binds) == 0 {
		return nil, false
	}
	k := msg.Keystroke()
	if len(m.pending) > 0 && k == "esc" {
		m.pending = nil
		return nil, true
	}
	keys := append(slices.Clip(m.pending), k)
	if len(m.pending) > 0 && !m.bound(keys) {
		// ctrl+f, then f, also pressed with ctrl held all along: ctrl+ff.
		// The first key's modifiers on a later key are the hand, not a
		// different chord, unless that chord is bound itself.
		first := m.pending[0]
		keys[len(keys)-1] = strings.TrimPrefix(k, first[:strings.LastIndexByte(first, '+')+1])
	}

	var next []string // what may follow keys, for the status line
	for _, b := range m.binds {
		switch {
		case slices.Equal(b.keys, keys):
			m.pending = nil
			m.tags = m.tags.Toggle(b.tag)
			return m.tagsChanged(), true
		case len(b.keys) > len(keys) && slices.Equal(b.keys[:len(keys)], keys):
			next = append(next, b.keys[len(keys)]+" "+b.tag.String())
		}
	}
	if len(next) > 0 {
		m.pending = keys
		m.setNotice(chord(keys)+"   "+strings.Join(next, "   "), false)
		return nil, true
	}
	if len(m.pending) > 0 {
		m.pending = nil
		m.setNotice(chord(keys)+" is not bound", true)
		return nil, true
	}
	return nil, false
}
