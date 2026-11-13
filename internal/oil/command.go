package oil

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Action is what a key asks of the interface. The buffer never touches
// the disk or the screen: writing, leaving and opening are the
// interface's to do.
type Action int

// Actions, from the commands of Vim and the keys of Oil.nvim.
const (
	None      Action = iota
	Write            // :w
	Quit             // :q: quit the program if there is nothing to write
	ForceQuit        // :q!, ZQ: quit the program, dropping the changes
	WriteQuit        // :wq, :x, ZZ: write if there is anything, then quit
	Close            // esc, q, ctrl+o: back to where the buffer was opened from

	Open         // enter: open the file or go into the folder on the line
	Parent       // -: the folder above
	Cwd          // _: the folder fndr started in
	Refresh      // ctrl+r: read the folder again
	ToggleHidden // g.: show or hide names starting with "."
	CopyPath     // gp: copy the path of the entry on the line
	Reveal       // gx: show the entry on the line in the file manager, as gx opens in Vim
	Help         // g?: the keys
)

// ErrCommand is returned for a command line that is not a command.
var ErrCommand = errors.New("not an editor command")

// keyActions are the keys of the owner's Oil.nvim setup, and esc, q and
// ctrl+o to close the buffer.
//
// The buffer fills the screen, as a single window in Vim, where :q quits
// Vim itself: so :q and its kin quit the program, and closing the buffer
// alone is esc or q, a step back. esc closes only with no command typed:
// "3<esc>" or "d<esc>" drop the command, as in Vim (see key).
var keyActions = map[string]Action{
	"enter":  Open,
	"-":      Parent,
	"_":      Cwd,
	"ctrl+r": Refresh,
	"g.":     ToggleHidden,
	"gp":     CopyPath,
	"gx":     Reveal,
	"g?":     Help,
	"q":      Close,
	"esc":    Close,
	"ctrl+o": Close, // the key that opened it closes it: a switch
	"ZZ":     WriteQuit,
	"ZQ":     ForceQuit,
}

// CmdLine returns what is typed after ":" in Command mode.
func (b *Buffer) CmdLine() string { return b.cmdline }

// cmdKey applies a key in Command mode: the line after ":" is edited
// until enter runs it or esc drops it.
func (b *Buffer) cmdKey(k string) (Action, error) {
	switch k {
	case "enter":
		b.mode = Normal
		return b.run(b.cmdline)
	case "esc", "ctrl+c":
		b.mode = Normal
	case "backspace":
		if b.cmdline == "" {
			b.mode = Normal // as in Vim: backspace on an empty line leaves it
			break
		}
		_, size := utf8.DecodeLastRuneInString(b.cmdline)
		b.cmdline = b.cmdline[:len(b.cmdline)-size]
	case "space":
		b.cmdline += " "
	default:
		if utf8.RuneCountInString(k) == 1 {
			b.cmdline += k
		}
	}
	return None, nil
}

// run carries out a command line.
func (b *Buffer) run(cmd string) (Action, error) {
	cmd = strings.TrimSpace(cmd)
	switch cmd {
	case "":
		return None, nil
	case "w":
		return Write, nil
	case "q":
		return Quit, nil
	case "q!":
		return ForceQuit, nil
	case "wq", "x":
		return WriteQuit, nil
	}
	if n, err := strconv.Atoi(cmd); err == nil { // ":42" goes to line 42
		b.GoTo(n - 1)
		return None, nil
	}
	return None, fmt.Errorf("%w: %s", ErrCommand, cmd)
}
