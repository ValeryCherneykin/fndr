package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/oil"
)

// oilModel is browseModel in OIL mode, with a hidden file beside the
// usual tree: README.md, docs/, src/, .git/, .env.
func oilModel(t *testing.T) (m *Model, root string) {
	t.Helper()
	m, root = browseModel(t, "")
	if err := os.WriteFile(filepath.Join(root, ".env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	key(m, 'o', tea.ModCtrl)
	if m.oil == nil {
		t.Fatal("ctrl+o did not enter OIL")
	}
	return m, root
}

// oilTexts returns the buffer's lines.
func (m *Model) oilTexts() []string {
	ls := m.oil.buf.Lines()
	ts := make([]string, len(ls))
	for i, l := range ls {
		ts[i] = l.Text
	}
	return ts
}

// command types a command line and runs it: command(m, "q!"). It reports
// whether fndr quit.
func command(m *Model, cmd string) (quit bool) {
	typeText(m, ":"+cmd)
	return key(m, tea.KeyEnter, 0)
}

// TestOilShowsFolder: the buffer is the folder as Oil shows it: folders
// first with a trailing "/", hidden names and .git left out.
func TestOilShowsFolder(t *testing.T) {
	m, _ := oilModel(t)
	if got, want := m.oilTexts(), []string{"docs/", "src/", "README.md"}; !slices.Equal(got, want) {
		t.Errorf("buffer %q, want %q", got, want)
	}
	view := m.View()
	for _, s := range []string{" NORMAL ", "README.md", "src/"} {
		if !strings.Contains(view.Content, s) {
			t.Errorf("view lacks %q", s)
		}
	}
	lines := strings.Split(view.Content, "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d is %d cells, want %d", i, w, m.width)
		}
	}
}

// TestOilLeavesOutWhatItHides: what the buffer does not show must not be
// in the plan's items either, or its missing line would read as "delete
// it" and :w would trash .git.
func TestOilLeavesOutWhatItHides(t *testing.T) {
	m, _ := oilModel(t)
	for _, it := range m.oil.items {
		if it.Name == ".git/" || it.Name == ".env" {
			t.Errorf("item %q is hidden, yet the plan would see it", it.Name)
		}
	}
	if m.oil.changes != 0 {
		t.Errorf("%d changes in a buffer nobody touched", m.oil.changes)
	}
}

// TestOilClose: esc, q and ctrl+o again step back to search, without
// quitting fndr. esc after a half-typed command drops the command only,
// as in Vim, and from Insert mode it goes back to Normal first.
func TestOilClose(t *testing.T) {
	for _, leave := range []func(m *Model) bool{
		func(m *Model) bool { return key(m, tea.KeyEscape, 0) },
		func(m *Model) bool { return key(m, 'q', 0) },
		func(m *Model) bool { return key(m, 'o', tea.ModCtrl) },
	} {
		m, _ := oilModel(t)
		if quit := leave(m); quit || m.oil != nil {
			t.Errorf("quit %v, in OIL %v; want back in search", quit, m.oil != nil)
		}
	}

	m, _ := oilModel(t)
	for _, keys := range []string{"3\x1b", "d\x1b", "i\x1b"} {
		for _, r := range keys {
			if r == '\x1b' {
				key(m, tea.KeyEscape, 0)
			} else {
				typeText(m, string(r))
			}
		}
		if m.oil == nil {
			t.Fatalf("%q closed OIL; esc should only drop what was typed", keys)
		}
	}
}

// TestOilQuit: :q quits fndr, as :q in Vim's only window quits Vim.
func TestOilQuit(t *testing.T) {
	m, _ := oilModel(t)
	if !command(m, "q") {
		t.Error(":q on an untouched buffer did not quit fndr")
	}
}

// TestOilQuitRefusesChanges: neither :q nor esc ever drops an edit; :q!
// does, on purpose. An edit undone is no edit.
func TestOilQuitRefusesChanges(t *testing.T) {
	m, _ := oilModel(t)
	typeText(m, "dd")
	if m.oil.changes != 1 || !strings.Contains(m.View().Content, "1 change") {
		t.Errorf("changes %d; the status bar should say 1 change", m.oil.changes)
	}
	if command(m, "q") || m.oil == nil || !m.noticeErr {
		t.Fatal(":q quit or left OIL with a change unwritten")
	}
	if key(m, tea.KeyEscape, 0) || m.oil == nil {
		t.Fatal("esc left OIL with a change unwritten")
	}

	typeText(m, "u")
	if !command(m, "q") {
		t.Fatal(":q after undoing everything did not quit")
	}

	m, _ = oilModel(t)
	typeText(m, "dd")
	if !command(m, "q!") {
		t.Error(":q! did not quit")
	}
}

// TestOilTyping: keys reach the buffer as Vim keys, and the mode shows.
func TestOilTyping(t *testing.T) {
	m, _ := oilModel(t)
	typeText(m, "GA.bak")
	if m.oil.buf.Mode() != oil.Insert || !strings.Contains(m.View().Content, " INSERT ") {
		t.Error("A did not start typing")
	}
	key(m, tea.KeyEscape, 0)
	if got := m.oilTexts()[2]; got != "README.md.bak" {
		t.Errorf("line %q, want README.md.bak", got)
	}
	if m.oil == nil {
		t.Fatal("esc left OIL: it should only leave Insert mode")
	}

	// The terminal's cursor is on the buffer's: after the name's last
	// character, on the third line.
	c := m.View().Cursor
	if c.Y != 2+2 {
		t.Errorf("cursor on screen row %d, want 4", c.Y)
	}
}

func TestOilBadCommand(t *testing.T) {
	m, _ := oilModel(t)
	command(m, "nope")
	if !m.noticeErr || !strings.Contains(m.notice, "nope") {
		t.Errorf("notice %q, want an error naming the command", m.notice)
	}
}

// TestOilScrolls: in a folder taller than the screen the cursor stays in
// view, with lines around it.
func TestOilScrolls(t *testing.T) {
	m, root := oilModel(t)
	for i := range 60 {
		if err := os.WriteFile(filepath.Join(root, "f"+string(rune('a'+i%26))+strings.Repeat("x", i/26)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	typeText(m, "q") // back to search, and in again to read the new files
	key(m, 'o', tea.ModCtrl)
	typeText(m, "G")
	h, row := m.listHeight(), m.oil.buf.Row()
	if row < m.oil.offset || row >= m.oil.offset+h {
		t.Errorf("cursor row %d off screen [%d, %d)", row, m.oil.offset, m.oil.offset+h)
	}
	typeText(m, "gg")
	if m.oil.offset != 0 {
		t.Errorf("offset %d at the top", m.oil.offset)
	}
}

// TestOilEveryScreenSize: in every mode and at every size, the screen is
// exactly height lines of exactly width cells, and nothing panics.
func TestOilEveryScreenSize(t *testing.T) {
	for _, keys := range []string{"", "GA.bak", ":wq", "dd", "plan"} {
		m, _ := oilModel(t)
		if keys == "plan" { // the plan's box over the buffer
			typeText(m, "ddGonew-file-with-a-long-name.txt")
			key(m, tea.KeyEscape, 0)
			command(m, "w")
		} else {
			typeText(m, keys)
		}
		for w := 1; w <= 100; w++ {
			for h := 4; h <= 12; h++ {
				m.Update(tea.WindowSizeMsg{Width: w, Height: h})
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != h {
					t.Fatalf("%q %dx%d: %d lines", keys, w, h, len(lines))
				}
				for i, l := range lines {
					if lw := ansi.StringWidth(l); lw != w {
						t.Fatalf("%q %dx%d: line %d is %d cells", keys, w, h, i, lw)
					}
				}
			}
		}
	}
}

// writeModel is oilModel with the trash in a temporary home, so trashing
// in a test never touches the real one.
func writeModel(t *testing.T) (m *Model, root, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	m, root = oilModel(t)
	return m, root, home
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// indexed reports whether the index holds path.
func (m *Model) indexed(path string) bool {
	for _, e := range m.ix.Snapshot() {
		if e.Path == path {
			return true
		}
	}
	return false
}

// TestOilRename: change a name, :w shows the plan and changes nothing, y
// renames on the disk; the buffer is read again with the cursor on the
// new name, and search knows it.
func TestOilRename(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "G")
	typeText(m, "ccNOTES.md")
	key(m, tea.KeyEscape, 0)
	command(m, "w")

	view := m.View().Content
	for _, s := range []string{"Apply 1 change?", "rename", "README.md → NOTES.md", " CONFIRM "} {
		if !strings.Contains(view, s) {
			t.Errorf("plan screen lacks %q", s)
		}
	}
	if !exists(filepath.Join(root, "README.md")) {
		t.Fatal("the disk changed before y")
	}

	typeText(m, "y")
	if exists(filepath.Join(root, "README.md")) || !exists(filepath.Join(root, "NOTES.md")) {
		t.Fatal("y did not rename README.md to NOTES.md")
	}
	if m.oil == nil || m.oil.plan != nil {
		t.Fatal("after y: want the buffer again, in OIL")
	}
	if got := m.oilTexts()[m.oil.buf.Row()]; got != "NOTES.md" {
		t.Errorf("cursor on %q, want the renamed NOTES.md", got)
	}
	if m.notice != "renamed 1" || m.oil.changes != 0 {
		t.Errorf("notice %q, changes %d", m.notice, m.oil.changes)
	}
	if m.indexed(filepath.Join(root, "README.md")) || !m.indexed(filepath.Join(root, "NOTES.md")) {
		t.Error("the index still has the old name, or not the new one")
	}
}

// TestOilCreate: a new line with a path creates the folders on the way.
func TestOilCreate(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "Gobin/run.sh")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")

	if !exists(filepath.Join(root, "bin", "run.sh")) {
		t.Fatal("bin/run.sh was not created")
	}
	if got := m.oilTexts()[m.oil.buf.Row()]; got != "bin/" {
		t.Errorf("cursor on %q, want bin/", got)
	}
	if !m.indexed(filepath.Join(root, "bin")) || !m.indexed(filepath.Join(root, "bin", "run.sh")) {
		t.Error("search does not know bin/run.sh")
	}
}

// TestOilTrash: dd and :w move the entry to the trash, never delete it.
func TestOilTrash(t *testing.T) {
	m, root, home := writeModel(t)
	typeText(m, "dd") // docs/
	command(m, "w")
	if !strings.Contains(m.View().Content, "trash   docs/") {
		t.Error("the plan does not show the trash")
	}
	typeText(m, "y")
	if exists(filepath.Join(root, "docs")) {
		t.Fatal("docs is still there")
	}
	mac, linux := filepath.Join(home, ".Trash", "docs"), filepath.Join(home, ".local", "share", "Trash", "files", "docs")
	if !exists(mac) && !exists(linux) {
		t.Error("docs is gone but not in the trash")
	}
	if m.notice != "trashed 1" {
		t.Errorf("notice %q", m.notice)
	}
}

// TestOilPlanDeclined: n goes back to the buffer with the edit intact,
// and nothing on the disk has changed.
func TestOilPlanDeclined(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "dd")
	command(m, "w")
	typeText(m, "n")
	if m.oil.plan != nil || !exists(filepath.Join(root, "docs")) {
		t.Fatal("n did not go back, or the disk changed")
	}
	if m.oil.changes != 1 {
		t.Errorf("changes %d, want the edit kept", m.oil.changes)
	}
}

// TestOilBadLine: a buffer that cannot become a plan says why, with the
// cursor on the line to blame, and shows no plan.
func TestOilBadLine(t *testing.T) {
	m, _, _ := writeModel(t)
	typeText(m, "Gonew")
	key(m, tea.KeyEscape, 0)
	typeText(m, "yyp") // "new" twice
	command(m, "w")
	if m.oil.plan != nil {
		t.Fatal("a plan for two lines with one name")
	}
	if !m.noticeErr || !strings.Contains(m.notice, "line 5") || m.oil.buf.Row() != 4 {
		t.Errorf("notice %q, cursor on row %d; want line 5 named and the cursor on it", m.notice, m.oil.buf.Row())
	}
}

func TestOilNothingToWrite(t *testing.T) {
	m, _, _ := writeModel(t)
	command(m, "w")
	if m.oil.plan != nil || m.notice != "nothing to write" {
		t.Errorf("plan %v notice %q", m.oil.plan, m.notice)
	}
	if !command(m, "wq") {
		t.Error(":wq with nothing to write did not quit")
	}
}

// TestOilWriteQuit: :wq writes, then quits fndr, as it quits Vim.
func TestOilWriteQuit(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "G")
	typeText(m, "ccNOTES.md")
	key(m, tea.KeyEscape, 0)
	if command(m, "wq") {
		t.Fatal(":wq quit before the plan was confirmed")
	}
	if !key(m, 'y', 0) {
		t.Fatal(":wq and y did not quit fndr")
	}
	if !exists(filepath.Join(root, "NOTES.md")) {
		t.Error("quit without writing NOTES.md")
	}
}

// TestOilRenameFolder: the files in a renamed folder are found under its
// new name, and no longer under the old one.
func TestOilRenameFolder(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "j") // src/
	typeText(m, "cclib/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !exists(filepath.Join(root, "lib", "main.go")) {
		t.Fatal("src was not renamed to lib")
	}
	if m.indexed(filepath.Join(root, "src", "main.go")) || !m.indexed(filepath.Join(root, "lib", "main.go")) {
		t.Error("search still finds src/main.go, or not lib/main.go")
	}
}

// TestOilCreateHidden: a new hidden file is shown, and selected, though
// hidden names were off: what was just made never vanishes.
func TestOilCreateHidden(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "Go.secret")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !exists(filepath.Join(root, ".secret")) {
		t.Fatal(".secret was not created")
	}
	if got := m.oilTexts()[m.oil.buf.Row()]; got != ".secret" {
		t.Errorf("cursor on %q, want the new .secret, shown", got)
	}
}

// TestOilWalk: enter goes into a folder, - comes back out onto it, _
// returns to where fndr started; fndr's folder follows, so leaving OIL
// shows the folder that was being edited.
func TestOilWalk(t *testing.T) {
	m, root := oilModel(t)
	typeText(m, "j") // src/
	key(m, tea.KeyEnter, 0)
	if got, want := m.oilTexts(), []string{"main.go", "util.go"}; !slices.Equal(got, want) {
		t.Fatalf("after enter on src/: %q, want %q", got, want)
	}
	if m.cwd != filepath.Join(root, "src") {
		t.Errorf("cwd %q: fndr did not follow OIL into src", m.cwd)
	}

	typeText(m, "-")
	if got := m.oilTexts()[m.oil.buf.Row()]; got != "src/" || m.oil.dir != root {
		t.Errorf("after -: in %q on %q, want the root, on src/", m.oil.dir, got)
	}

	typeText(m, "j")
	key(m, tea.KeyEnter, 0)
	typeText(m, "_")
	if m.oil.dir != root {
		t.Errorf("_ went to %q, want %q, where fndr started", m.oil.dir, root)
	}
}

// TestOilEnterOnFileOpens: enter on a file opens it and stays in OIL.
func TestOilEnterOnFileOpens(t *testing.T) {
	m, root := oilModel(t)
	m.oil.buf.GoTo(2) // README.md
	if cmd := m.oilGo(oil.Open); cmd == nil {
		t.Error("enter on a file returned no command")
	}
	if m.oil == nil || m.oil.dir != root {
		t.Error("opening a file left OIL or the folder")
	}
}

// TestOilHiddenAndRefresh: g. shows and hides dot names; ctrl+r reads the
// folder again, finding what changed behind fndr's back. The cursor stays
// on its line through both.
func TestOilHiddenAndRefresh(t *testing.T) {
	m, root := oilModel(t)
	typeText(m, "G") // README.md
	typeText(m, "g.")
	if !slices.Contains(m.oilTexts(), ".env") || slices.Contains(m.oilTexts(), ".git/") {
		t.Errorf("g.: %q, want .env shown and .git never", m.oilTexts())
	}
	if got := m.oilTexts()[m.oil.buf.Row()]; got != "README.md" {
		t.Errorf("cursor moved to %q", got)
	}
	typeText(m, "g.")
	if slices.Contains(m.oilTexts(), ".env") {
		t.Error("g. again did not hide .env")
	}

	if err := os.WriteFile(filepath.Join(root, "later.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	key(m, 'r', tea.ModCtrl)
	if !slices.Contains(m.oilTexts(), "later.txt") {
		t.Errorf("ctrl+r: %q, want later.txt read in", m.oilTexts())
	}
}

// TestOilKeepsEditsWhenLeaving: an edit not written is never dropped by
// going elsewhere or back to search; :w, u or :q! decide.
func TestOilKeepsEditsWhenLeaving(t *testing.T) {
	m, root := oilModel(t)
	typeText(m, "jdd") // src/ deleted in the buffer, cursor on README.md
	for _, k := range []func(){
		func() { key(m, tea.KeyEnter, 0) },
		func() { typeText(m, "-") },
		func() { typeText(m, "_") },
		func() { key(m, 'r', tea.ModCtrl) },
		func() { typeText(m, "g.") },
		func() { key(m, tea.KeyEscape, 0) },
		func() { typeText(m, "q") },
		func() { key(m, 'o', tea.ModCtrl) },
	} {
		k()
		if m.oil == nil || m.oil.dir != root || m.oil.changes != 1 || !m.noticeErr {
			t.Fatalf("a move with an unwritten edit: dir %q, changes %d, notice %q", m.oil.dir, m.oil.changes, m.notice)
		}
	}
}

// TestOilFailedWriteKeepsTheEdit: creating a name a hidden file already
// has fails on the disk. Nothing was done, so the buffer keeps the line as
// typed, to be fixed rather than typed again.
func TestOilFailedWriteKeepsTheEdit(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "Go.env") // there already, hidden
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !m.noticeErr || !strings.Contains(m.notice, "0 of 1 done") {
		t.Errorf("notice %q, want the failure", m.notice)
	}
	if !slices.Contains(m.oilTexts(), ".env") || m.oil.changes != 1 {
		t.Errorf("buffer %q, changes %d: the typed line is lost", m.oilTexts(), m.oil.changes)
	}
	if fi, err := os.Stat(filepath.Join(root, ".env")); err != nil || fi.Size() != 0 {
		t.Error("the existing .env was touched")
	}
}

// BenchmarkOilKey is one key in OIL on a big folder, all of it: the
// buffer, the plan recount that runs after every key, and drawing the
// screen. Holding j must never lag.
func BenchmarkOilKey(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			es := make([]indexer.Entry, n)
			for i := range es {
				es[i] = indexer.NewEntry(fmt.Sprintf("/big/file-%05d.go", i), 1234, 0, false)
			}
			m := newModel("/big", "", &indexer.Index{})
			b.Cleanup(m.stop)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			m.Update(oilMsg{dir: "/big", entries: es})
			j := tea.KeyPressMsg{Code: 'j', Text: "j"}
			for b.Loop() {
				m.Update(j)
				_ = m.View()
			}
		})
	}
}
