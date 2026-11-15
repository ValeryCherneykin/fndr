package tui

import (
	"fmt"

	"github.com/ValeryCherneykin/fndr/internal/config"
)

// The config, taken in: the keys (keys.go), the chords (binds.go), the
// folders (folders.go). The switches, which need no checking, are set in
// New.

// useConfig takes the settings read from the config file, and says its
// mistakes at once, the file's (errs) and those only the interface can
// see: a bind that silently does nothing is worse than a line of red.
func (m *Model) useConfig(cfg *config.Config, errs []error) {
	m.useKeys(cfg.Keys) // first: a chord may not start on an action's key
	errs = append(errs, m.useBinds(cfg.Binds)...)
	errs = append(errs, m.useFolders(cfg.Skip, cfg.Include)...)
	if len(errs) == 0 {
		return
	}
	msg := "config " + errs[0].Error()
	if len(errs) > 1 {
		msg += fmt.Sprintf(" (and %d more)", len(errs)-1)
	}
	m.setNotice(msg, true)
}
