package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/config"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

type tutorState struct {
	step, footerHeight int
	trash              string
	writes             sync.Mutex
	closed             bool
}

// NewTutor creates a practice workspace inside an empty directory owned by the caller.
// Call Close before removing the directory so in-flight writes can finish.
func NewTutor(dir string) (*Model, error) {
	root := filepath.Join(dir, "practice")
	guide := "# Field notes\n\n" + strings.Repeat("Search by name, then narrow by type.\n\n", 24)
	fixtures := map[string]string{
		".gitignore":                "build/\n",
		".project":                  "fndr tutor\n",
		"build/generated.log":       "Generated output\n",
		"docs/field-notes.md":       guide,
		"docs/field-report.md":      "# Report\n",
		"docs/map.txt":              "North trail\n",
		"projects/atlas/main.go":    "package main\n\nfunc main() {}\n",
		"projects/atlas/app.json":   "{\"name\":\"atlas\"}\n",
		"projects/atlas/Dockerfile": "FROM scratch\n",
		"practice/draft.txt":        "Rename me in the tutorial.\n",
		"practice/source.txt":       "Copy me in Oil.\n",
		"practice/spare.txt":        "Trash me in Oil and bring me back.\n",
	}
	for name, contents := range fixtures {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			return nil, err
		}
	}
	cfg := config.Default()
	cfg.History, cfg.SaveIndex, cfg.Preview = false, false, false
	for _, editor := range []string{"nvim", "vim", "vi"} {
		if _, err := exec.LookPath(editor); err == nil {
			cfg.Editor = editor
			if editor != "vi" {
				cfg.Editor += " -u NONE -i NONE -n"
			}
			break
		}
	}
	m := New(root, &cfg, nil)
	m.home = root
	m.tutor = &tutorState{trash: filepath.Join(dir, "trash")}
	return m, nil
}

func (t *tutorState) skip(key tea.KeyPressMsg) bool {
	if key.String() != "f6" || t.step >= len(tutorLessons) {
		return false
	}
	t.step++
	return true
}

func (t *tutorState) advance(m *Model, msg tea.Msg) {
	if t.step < len(tutorLessons) {
		if done := tutorLessons[t.step].done; done != nil && done(m, msg) {
			t.step++
		}
	}
}

func (t *tutorState) footer(width int) string {
	title, hint := "Course complete", "Keep practicing here or press Ctrl+C to exit. Run fndr tutor for a fresh workspace."
	label, keys := " TUTOR ", "  ·  Ctrl+C exit"
	if t.step < len(tutorLessons) {
		lesson := tutorLessons[t.step]
		label = fmt.Sprintf(" TUTOR %02d/%02d ", t.step+1, len(tutorLessons))
		title, hint = lesson.title, lesson.hint
		keys = "  ·  F6 skip  ·  Ctrl+C exit"
		if lesson.done == nil {
			keys = "  ·  F6 continue  ·  Ctrl+C exit"
		}
	}
	rows := []string{stPrompt.Render(strings.Repeat("─", width))}
	if t.footerHeight >= 2 {
		rows = append(rows, finish(" "+stMode.Render(label)+"  "+stQuery.Render(title)+keys, width))
	}
	if t.footerHeight > 2 {
		for line := range strings.SplitSeq(ansi.Wrap(hint, max(1, width-2), ""), "\n") {
			rows = append(rows, finish(" "+line, width))
			if len(rows) == t.footerHeight {
				break
			}
		}
	}
	for len(rows) < t.footerHeight {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) tutorAllows(path string) bool {
	if m.tutor == nil || tutorPath(m.start, path) {
		return true
	}
	m.setNotice("choose a path inside the tutor workspace", true)
	return false
}

func tutorPath(root, path string) bool {
	path = filepath.Clean(path)
	if !within(path, root) {
		return false
	}
	for path != root {
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 || err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false
		}
		path = filepath.Dir(path)
	}
	return true
}

func (m *Model) tutorPlan(dir string, plan mutation.Plan) bool {
	if m.tutor == nil {
		return true
	}
	for _, op := range plan {
		for _, name := range []string{op.From, op.To} {
			if name != "" && !m.tutorAllows(mutation.Target(dir, name)) {
				return false
			}
		}
	}
	return true
}

func (t *tutorState) beginWrite() (func(), error) {
	if t == nil {
		return func() {}, nil
	}
	t.writes.Lock()
	if t.closed {
		t.writes.Unlock()
		return nil, errors.New("tutor workspace closed")
	}
	return t.writes.Unlock, nil
}

func (t *tutorState) close() {
	t.writes.Lock()
	t.closed = true
	t.writes.Unlock()
}
