// Package shell holds the shell integration printed by "fndr init": a
// function that runs fndr and then changes the shell's directory to where
// the user left off.
//
// A program cannot do that itself. Every process has its own working
// directory, copied from its parent when it starts; fndr changing its own
// copy is invisible to the shell that launched it, which is why cd is a
// shell builtin and not a program. So fndr writes the folder to a file
// (-cwd-file), and a function running inside the shell reads it and cds.
package shell

import (
	_ "embed" // for the //go:embed scripts below
	"fmt"
)

// The scripts live in their own files so they can be read, edited and
// checked as shell code, and are compiled into the binary by go:embed.
//
//go:embed fndr.zsh
var zsh string

// Init returns the integration script for the named shell.
func Init(name string) (string, error) {
	switch name {
	case "zsh":
		return zsh, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: zsh)", name)
	}
}
