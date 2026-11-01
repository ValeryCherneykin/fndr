// Command fndr is a search-first terminal file manager.
//
// Usage:
//
//	fndr [flags]
//
// The flags are:
//
//	-version
//		Print the version and exit.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the real entry point. It takes its arguments and output streams as
// parameters and returns an exit code instead of calling os.Exit, so tests can
// drive the whole CLI in-process without spawning a binary.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fndr", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")

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

	// Explicitly ignored: if stderr itself is broken there is nowhere left to
	// report the failure.
	_, _ = fmt.Fprintln(stderr, "fndr: the interface is not implemented yet; try -version")
	return 0
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
