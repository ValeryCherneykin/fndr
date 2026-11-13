package opener

import (
	"errors"
	"path/filepath"
	"runtime"
)

// ErrNoOpener means the system has no known way to open a file in its
// default application.
var ErrNoOpener = errors.New("no system opener found: install xdg-utils")

// linuxOpeners hand a file to the desktop's default application, in order
// of preference. xdg-open is the freedesktop standard; gio ships with GNOME
// and still works where xdg-utils is missing.
var linuxOpeners = [][]string{
	{"xdg-open"},
	{"gio", "open"},
}

// System returns the command line that opens path in the operating
// system's default application, the way a double-click in Finder would:
// a PDF in the PDF viewer, an image in the image viewer.
func System(path string) ([]string, error) {
	return osEnv.system(runtime.GOOS, path)
}

func (e env) system(goos, path string) ([]string, error) {
	switch goos {
	case "darwin":
		// open ships with every macOS and returns once the app has the file.
		return []string{"open", path}, nil
	case "linux":
		// Under WSL the desktop is Windows: xdg-open, if present at all,
		// has no Linux desktop to talk to. wslview hands the file over.
		if e.getenv("WSL_DISTRO_NAME") != "" {
			if _, err := e.lookPath("wslview"); err == nil {
				return []string{"wslview", path}, nil
			}
		}
		for _, cmd := range linuxOpeners {
			if _, err := e.lookPath(cmd[0]); err == nil {
				// The table entries are exactly full (len == cap), so append
				// copies rather than writing into the shared table.
				return append(cmd, path), nil
			}
		}
	}
	return nil, ErrNoOpener
}

// Reveal returns the command line that shows path in the system's file
// manager, Finder on macOS: a folder as its window, anything else
// selected in its folder's window, as Finder's "Show in Enclosing Folder".
func Reveal(path string, isDir bool) ([]string, error) {
	return osEnv.reveal(runtime.GOOS, path, isDir)
}

func (e env) reveal(goos, path string, isDir bool) ([]string, error) {
	if goos == "darwin" {
		// A folder with an extension is opened by open as what it is:
		// Ghostty.app would start Ghostty, and a .photoslibrary would open
		// Photos. -R shows it instead, selected, whatever it is.
		if isDir && filepath.Ext(path) == "" {
			return []string{"open", path}, nil
		}
		return []string{"open", "-R", path}, nil
	}
	// Elsewhere a file manager is a folder's default application, and
	// there is no common way to select a file in it: the file's folder.
	if !isDir {
		path = filepath.Dir(path)
	}
	return e.system(goos, path)
}
