package tui

import (
	"path/filepath"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
	"github.com/ValeryCherneykin/fndr/internal/oil"
)

// What is yanked in OIL outlives its buffer, as Oil.nvim's does: yy in one
// folder, then off anywhere (other folders, GLOBAL, a search), and p in
// another buffer puts it there; :w copies it. A buffer's register holds
// line ids, which mean nothing in the next one, so on the way out it is
// kept by entry (carry), and on the way in each entry is found again: one
// of the new buffer's own keeps its id, as yy p in one folder always did,
// and one from elsewhere joins the buffer as an Away item, a copy for
// every line of it (mutation.Item).
//
// Only a copy: a line dd'd and put in another folder is not a move. Leaving
// a buffer with an edit asks for :w first, by which the dd'd entry is in
// the trash; a move is the line rewritten as a path ("~/b/x").

// carried is a register kept by entry while fndr runs.
type carried struct {
	linewise bool
	lines    []carriedLine
	text     string // from inside a line
}

// carriedLine is a line of a register: an entry, or text typed.
type carriedLine struct {
	entry *indexer.Entry // nil for a line typed, not an entry's
	text  string
}

// carry is the buffer's register, kept by entry for the next buffer.
func (st *oilState) carry() carried {
	r := st.buf.Register()
	c := carried{linewise: r.Linewise, text: r.Text}
	for _, l := range r.Lines {
		cl := carriedLine{text: l.Text}
		if l.ID > 0 && l.ID <= len(st.entries) {
			e := st.entries[l.ID-1]
			cl.entry = &e
		}
		c.lines = append(c.lines, cl)
	}
	return c
}

// carryIn gives a fresh buffer the register c, its entries found again:
// those of the buffer by their ids, others added as Away items, named by
// their whole path and put by their name, as Oil.nvim puts them.
func (st *oilState) carryIn(c carried) {
	if !c.linewise {
		st.buf.SetRegister(oil.Register{Text: c.text})
		return
	}
	ids := make(map[string]int, len(st.entries))
	for i := range st.entries {
		ids[st.entries[i].Path] = i + 1
	}
	reg := oil.Register{Linewise: true}
	for _, cl := range c.lines {
		if cl.entry == nil {
			reg.Lines = append(reg.Lines, mutation.Line{Text: cl.text})
			continue
		}
		e := cl.entry
		id, here := ids[e.Path]
		if !here {
			id = len(st.items) + 1
			name := e.Path
			if e.IsDir {
				name += "/"
			}
			st.items = append(st.items, mutation.Item{ID: id, Name: name, Away: true})
			st.entries = append(st.entries, *e)
			ids[e.Path] = id
		}
		reg.Lines = append(reg.Lines, mutation.Line{ID: id, Text: st.putName(id)})
	}
	st.buf.SetRegister(reg)
}

// putName is what p writes for the item id: its line, or, for an Away
// item, its name with a folder's "/".
func (st *oilState) putName(id int) string {
	it := st.items[id-1]
	if !it.Away {
		return it.Name
	}
	name := filepath.Base(it.Name)
	if st.entries[id-1].IsDir {
		name += "/"
	}
	return name
}
