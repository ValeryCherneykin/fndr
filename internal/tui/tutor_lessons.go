package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

type tutorLesson struct {
	title, hint string
	done        func(*Model, tea.Msg) bool
}

var tutorLessons = []tutorLesson{
	{"Search · Find a file", "Start typing fldnt. It finds field-notes.md even though you left letters out.", tutorSearch("fldnt", "field-notes.md")},
	{"Search · Edit the query", "Press Backspace to erase the last character. Use Left and Right to edit in place, Ctrl+W to erase a word, or Ctrl+U to clear the text.", func(m *Model, _ tea.Msg) bool { return m.query == "fldn" }},
	{"Browse · Enter a folder", "Clear the query with Esc, type docs/ to find only folders, then press Enter on docs.", tutorFolder("docs")},
	{"Browse · Move the selection", "Use Down or Ctrl+N to select field-report.md. Up or Ctrl+P moves back. Page Up/Down and Home/End jump farther.", tutorSelected("field-report.md")},
	{"Preview · Read before opening", "Select field-notes.md and press Ctrl+V. The preview needs a terminal at least 100 columns wide.", func(m *Model, _ tea.Msg) bool {
		path := m.selectedPath()
		pane, loaded := m.paneCache[path]
		return filepath.Base(path) == "field-notes.md" && m.paneShown() && loaded && pane.err == nil
	}},
	{"Preview · Scroll the file", "Press Ctrl+J to read farther down. Ctrl+K scrolls back.", func(m *Model, _ tea.Msg) bool { return m.paneScroll > 0 }},
	{"Open · Edit a text file", "Press Enter on field-notes.md. Close the editor to return here. In Neovim, press Esc, type :q and press Enter.", func(_ *Model, msg tea.Msg) bool {
		opened, ok := msg.(openedMsg)
		return ok && opened.name == "field-notes.md" && opened.err == nil
	}},
	{"Browse · Go back", "Press - on an empty query to go up. The folder you left stays selected.", tutorFolder(".")},
	{"Filter · File extension", "Type .go and press Ctrl+T. It becomes a chip. Type after a chip to search only within its matches.", tutorTag(".go")},
	{"Filter · Path fragment", "Keep .go, type atlas and press Ctrl+T again. Stacked chips must all match.", tutorTag("atlas")},
	{"Filter · Remove one chip", "Press Esc to remove atlas. Typing a chip again and pressing Ctrl+T also toggles it off.", func(m *Model, _ tea.Msg) bool {
		return len(m.tags) == 1 && m.tags[0].String() == ".go"
	}},
	{"Filter · File kind", "Press Ctrl+U on an empty query to clear every chip. Type doc then Ctrl+T. Try image, video, audio, code, and archive the same way.", tutorTag("doc")},
	{"Filter · Files or folders", "Clear the chips with Ctrl+U, then add dirs with Ctrl+T. Use files for non-folders, or add / to a fuzzy query for folders only.", tutorTag("dirs")},
	{"Filter · Size", "Clear the chips, then add size>100b. Try <10k for a maximum or .md>100b to combine an extension with a size.", tutorTag("size>100b")},
	{"Filter · Modification time", "Clear the chips, then add @today. @yesterday, @week and @3d use calendar days.", tutorTag("@today")},
	{"Filter · Either condition", "Clear the chips, then add .go|dirs. The pipe lets a result match either side of one chip.", tutorTag(".go|dirs")},
	{"Filter · Find a project by its contents", "Clear the chips, then add has:.json. It finds the deepest folders that contain a JSON file, including projects/atlas.", tutorTag("has:.json")},
	{"Filter · Combine content conditions", "Keep has:.json and add has:dockerfile. Use this when you remember what a project contains but forgot its name.", tutorTag("has:dockerfile")},
	{"Scope · Search globally", "Clear the chips and enter docs again. Type main and press Tab to search the entire practice workspace. In regular fndr, GLOBAL searches your home.", func(m *Model, _ tea.Msg) bool {
		return m.globalOn && tutorMatches(m, "main", "main.go")
	}},
	{"Scope · Include ignored files", "Press Tab to return to LOCAL, clear the query, and press - to return to the workspace root. Type generated and press Ctrl+D to search ignored files.", func(m *Model, _ tea.Msg) bool {
		return m.wide != nil && tutorMatches(m, "generated", "generated.log")
	}},
	{"Paths · Copy what you found", "With generated.log selected, press Ctrl+Y then N to copy its name. Y copies the full path and R copies the relative path. Skip if your terminal has no clipboard.", func(_ *Model, msg tea.Msg) bool {
		copied, ok := msg.(copiedMsg)
		return ok && copied.text == "generated.log" && len(copied.copied) > 0
	}},
	{"Paths · Reveal in your file manager", "Ctrl+G reveals the selection in Finder or your Linux file manager. Try it if you have a desktop, then press F6.", nil},
	{"Change · Rename a file", "Press Ctrl+D to leave the ignored scope. Clear the query, type draft and press Ctrl+R. Replace the filename with ready.txt, keeping its folder path, and press Enter.", func(_ *Model, msg tea.Msg) bool {
		renamed, ok := msg.(renamedMsg)
		return ok && renamed.err == nil && filepath.Base(renamed.from) == "draft.txt" && filepath.Base(renamed.to) == "ready.txt"
	}},
	{"Change · Undo on disk", "Press Ctrl+Z and confirm with y. fndr restores the previous name.", tutorUndone(mutation.OpRename)},
	{"Oil · Edit a folder as text", "Clear the query. Type practice/ and enter that folder. Press Ctrl+O on an empty query to open its Oil buffer.", func(m *Model, _ tea.Msg) bool {
		return m.oil != nil && m.oil.dir == filepath.Join(m.start, "practice") && m.oil.search == nil
	}},
	{"Oil · Undo a buffer edit", "Move with j/k (or arrows). Try dd on a line, then u to bring it back in the buffer. U redoes an edit. Try it, restore the line, and press F6.", nil},
	{"Oil · Create a file", "Press G then o, type ideas.md, and press Esc. Type :w and press Enter to review the plan, then y to apply it. Add a trailing / when creating a folder.", tutorApplied(mutation.OpCreate, "ideas.md")},
	{"Oil · Copy by duplicating a line", "On source.txt, press yy then p. On the duplicate press cc, type backup.txt, then Esc. Use :w, Enter, y. The copy runs in the background.", tutorApplied(mutation.OpCopy, "backup.txt")},
	{"Oil · Trash a file", "Open Oil again with Ctrl+O. Select spare.txt and press dd. Use :w, Enter, y to discard it into the tutor's temporary trash.", tutorApplied(mutation.OpDelete, "spare.txt")},
	{"Oil · Restore a trashed file", "Leave Oil with Esc, then press Ctrl+Z and y to restore spare.txt.", tutorUndone(mutation.OpDelete)},
	{"Oil · Edit search results", "In the browser, type source and press Ctrl+O. This buffer contains matching paths, so you can work across folders. Press Esc to return.", func(m *Model, _ tea.Msg) bool {
		return m.oil != nil && m.oil.search != nil
	}},
	{"Oil · More Vim commands", "Try g. for hidden entries in a folder buffer, gp for its path, and gx to reveal it. Enter opens, - goes up, _ returns to the start, and :q exits fndr. Press F6 when ready.", nil},
	{"Tasks · Stop a copy", "To cancel a long copy, select its dimmed pending row and press Ctrl+X. If you quit during a copy, fndr asks whether to wait or stop. Press F6 to continue.", nil},
	{"Integrations · Shell and tmux", "In the browser try Ctrl+E and Ctrl+S. In regular fndr, Ctrl+E jumps with shell integration and Ctrl+S opens a folder session with tmux enabled. Press F6 when ready.", nil},
	{"Help · Find any key", "Return to the browser with Esc if needed and press F1. Help lists the active keys, including custom bindings in regular fndr.", func(m *Model, _ tea.Msg) bool { return m.help != nil }},
}

func tutorSearch(query, name string) func(*Model, tea.Msg) bool {
	return func(m *Model, _ tea.Msg) bool { return tutorMatches(m, query, name) }
}

func tutorMatches(m *Model, query, name string) bool {
	if m.query != query || m.searching {
		return false
	}
	for _, result := range m.results {
		if filepath.Base(result.Entry.Path) == name {
			return true
		}
	}
	return false
}

func tutorFolder(relative string) func(*Model, tea.Msg) bool {
	return func(m *Model, _ tea.Msg) bool { return m.cwd == filepath.Join(m.start, relative) }
}

func tutorSelected(name string) func(*Model, tea.Msg) bool {
	return func(m *Model, _ tea.Msg) bool { return filepath.Base(m.selectedPath()) == name }
}

func tutorTag(tag string) func(*Model, tea.Msg) bool {
	return func(m *Model, _ tea.Msg) bool {
		return len(m.tags) > 0 && m.tags[len(m.tags)-1].String() == tag
	}
}

func tutorApplied(kind mutation.Kind, name string) func(*Model, tea.Msg) bool {
	return func(_ *Model, msg tea.Msg) bool {
		var written oilWrittenMsg
		switch msg := msg.(type) {
		case oilWrittenMsg:
			written = msg
		case taskDoneMsg:
			written = msg.written
		default:
			return false
		}
		for _, step := range written.steps {
			path := step.To
			if kind == mutation.OpDelete {
				path = step.From
			}
			if step.Kind == kind && strings.EqualFold(filepath.Base(path), name) {
				return true
			}
		}
		return false
	}
}

func tutorUndone(kind mutation.Kind) func(*Model, tea.Msg) bool {
	return func(_ *Model, msg tea.Msg) bool {
		undone, ok := msg.(undoneMsg)
		return ok && undone.err == nil && undone.undone > 0 && len(undone.batch.Steps) == 1 && undone.batch.Steps[0].Kind == kind
	}
}
