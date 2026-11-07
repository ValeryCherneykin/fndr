// Package opener decides how a file is opened and builds the command that
// opens it: text goes to the user's editor, everything else to the
// operating system's default application, the way Spotlight or a
// double-click in Finder would.
//
// The package only builds commands; running them is the caller's job. The
// TUI has to suspend itself around a terminal editor, and that is a UI
// concern.
package opener
