// Command fndr is a search-first terminal file manager.
//
// Usage:
//
//	fndr [flags]
//	fndr init zsh|bash|fish
//	fndr config
//
// Without flags it opens the interface on the current directory: type to
// search, Ctrl+N and Ctrl+P (or up and down) to move through the list,
// Ctrl+H and Ctrl+L (or left and right) to move through the query, Enter to
// open a file or folder, "-" to go up, Esc to clear the query or quit,
// Ctrl+E to quit and jump to the selected folder, Ctrl+V to show or hide the
// preview, Ctrl+O to edit the folder as text with Vim keys (:w shows what
// will change, y applies it).
//
// The flags are:
//
//	-version
//		Print the version and exit.
//	-config path
//		Read settings from path instead of ~/.config/fndr/config (or
//		$XDG_CONFIG_HOME/fndr/config). With no file there on the first
//		start, fndr writes one: every setting and key, at its default,
//		each with a line on what it does.
//	-cwd-file path
//		On exit with Ctrl+E, write the folder to jump to to path, for a shell
//		function to cd into. A program cannot change its parent shell's
//		directory itself.
//
// "fndr init zsh|bash|fish" prints that shell function; add to ~/.zshrc:
//
//	eval "$(fndr init zsh)"
//
// to ~/.bashrc, the same with bash; to ~/.config/fish/config.fish:
//
//	fndr init fish | source
//
// "fndr config" prints the settings file as this fndr would write it, to
// see what a newer version knows, or to start over:
//
//	fndr config > ~/.config/fndr/config
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/config"
	"github.com/ValeryCherneykin/fndr/internal/shell"
	"github.com/ValeryCherneykin/fndr/internal/tui"
)

// version is set at build time by the linker:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/fndr
//
// It must be a package-level var, not a const: -X can only overwrite string
// variables, because constants are folded into the code at compile time and
// no longer exist as symbols the linker could patch.
var version string

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, runTUI))
}

// runTUI runs the interface on root, with the settings at configPath (""
// for the usual place), until the user quits.
func runTUI(root, configPath string) (string, error) {
	cfg, errs, written := loadConfig(configPath)
	model := tui.New(root, cfg, errs)
	if configPath == "" {
		configPath, _ = config.DefaultPath()
	}
	model.SetConfigPath(configPath, written != "")
	// 120 frames a second, Bubble Tea's most: a frame is ready as soon as a
	// key is handled, but goes out on the next tick, and a key that
	// searches waits for two (the letter, then the results). At 60, that
	// was some 5 ms more per key than the terminal's own echo; at 120,
	// measured through tmux, as quick as the echo. Idle, the ticker costs
	// about 0.35% more of one core.
	final, err := tea.NewProgram(model, tea.WithFPS(120)).Run()
	if err != nil {
		return "", err
	}
	m, ok := final.(*tui.Model)
	if !ok {
		return "", nil
	}
	// Losing the history or the index is not worth failing the jump the
	// user asked for: say so, and still change directory.
	if err := m.Close(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fndr: %v\n", err)
	}
	return m.ExitDir(), nil
}

// loadConfig reads the settings at configPath, "" for the usual place. On
// the first start there is no file in the usual place, and it is written,
// for the settings to be found where they are changed: written is its
// path then. A file named by -config is the user's, kept where they like:
// none there is the defaults, and nothing is written.
func loadConfig(configPath string) (cfg config.Config, errs []error, written string) {
	if configPath == "" {
		configPath, _ = config.DefaultPath() // no home: no settings, like no file
		if configPath == "" {
			return config.Default(), nil, ""
		}
		switch wrote, err := config.WriteNew(configPath); {
		case err != nil:
			errs = append(errs, fmt.Errorf("could not write %s: %w", configPath, err))
		case wrote:
			written = configPath
		}
	}
	cfg, loadErrs := config.Load(configPath)
	return cfg, append(errs, loadErrs...), written
}

// runInit prints the shell integration for the shell named in args.
func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: fndr init zsh|bash|fish")
		return 2
	}
	script, err := shell.Init(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fndr: %v\n", err)
		return 2
	}
	if _, err := io.WriteString(stdout, script); err != nil {
		return 1
	}
	return 0
}

// run is the real entry point. It takes its arguments and output streams as
// parameters and returns an exit code instead of calling os.Exit, so tests can
// drive the whole CLI in-process without spawning a binary. The interface is
// passed in as ui for the same reason: tests must not take over a terminal.
// ui runs the interface on root with the settings file at configPath and
// returns the folder the shell should change to afterwards, or "" to stay.
type ui func(root, configPath string) (exitDir string, err error)

func run(args []string, stdout, stderr io.Writer, ui ui) int {
	if len(args) > 0 && args[0] == "init" {
		return runInit(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "config" {
		if len(args) > 1 {
			_, _ = fmt.Fprintln(stderr, "usage: fndr config")
			return 2
		}
		if _, err := io.WriteString(stdout, config.Template()); err != nil {
			return 1
		}
		return 0
	}

	fs := flag.NewFlagSet("fndr", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	cwdFile := fs.String("cwd-file", "", "on exit with Ctrl+E, write the folder to jump to to this file")
	configPath := fs.String("config", "", "read settings from this file instead of ~/.config/fndr/config")

	if err := fs.Parse(args); err != nil {
		// -h and -help are a successful request for usage, not a mistake.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		// The flag package has already printed the error and usage.
		return 2
	}

	if *showVersion {
		// A failed write (closed pipe, full disk) means the caller did not get
		// the answer it asked for, so it must not look like success to scripts.
		if _, err := fmt.Fprintf(stdout, "fndr %s\n", resolveVersion()); err != nil {
			return 1
		}
		return 0
	}

	root, err := startDir()
	var exitDir string
	if err == nil {
		exitDir, err = ui(root, *configPath)
	}
	if err == nil && *cwdFile != "" && exitDir != "" {
		err = os.WriteFile(*cwdFile, []byte(exitDir), 0o600)
	}
	if err != nil {
		// Explicitly ignored: if stderr itself is broken there is nowhere
		// left to report the failure.
		_, _ = fmt.Fprintf(stderr, "fndr: %v\n", err)
		return 1
	}
	return 0
}

// startDir returns the directory to open: the current one, with symlinks
// resolved. The crawler refuses a root that is itself a symlink, and common
// directories are (/tmp on macOS is /private/tmp), so the real path is used.
func startDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(wd)
}

// resolveVersion reports the most precise version information available.
//
// Release builds get their version from -ldflags. A user who installs with
// `go install github.com/ValeryCherneykin/fndr/cmd/fndr@v0.1.0` gets no
// ldflags, but the Go toolchain embeds the module version in the binary, so
// we read it from the build info. Local `go build` produces "(devel)", which
// carries no useful information, so we report "dev" instead.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
