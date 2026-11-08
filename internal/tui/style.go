package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/ValeryCherneykin/fndr/internal/highlight"
)

// Palette is the "noir" scheme: near-black surfaces, greys for hierarchy, and
// one accent colour per scope. The hierarchy is the point. The file name is
// the brightest thing on a row, everything else steps down from it, so the eye
// lands on names first.
var (
	colBright  = lipgloss.Color("#fafafa") // selected name, titles
	colText    = lipgloss.Color("#d4d4d8") // names
	colDim     = lipgloss.Color("#71717a") // secondary text: directories, counts
	colMuted   = lipgloss.Color("#52525b") // tertiary text: hints
	colHair    = lipgloss.Color("#27272a") // hairlines
	colBar     = lipgloss.Color("#131316") // status bar background
	colLocal   = lipgloss.Color("#38bdf8") // LOCAL scope accent
	colLocalHi = lipgloss.Color("#7dd3fc") // matched characters
	colWarn    = lipgloss.Color("#f97316")
	colAlert   = lipgloss.Color("#ef4444")
)

// Styles are built once. A lipgloss.Style is a value, so these are safe to
// share and to derive from.
var (
	stPrompt = lipgloss.NewStyle().Foreground(colLocal).Bold(true)
	stQuery  = lipgloss.NewStyle().Foreground(colBright)
	stHair   = lipgloss.NewStyle().Foreground(colHair)
	stMarker = lipgloss.NewStyle().Foreground(colLocal)

	stName       = lipgloss.NewStyle().Foreground(colText)
	stNameSel    = lipgloss.NewStyle().Foreground(colBright).Bold(true)
	stNameHit    = lipgloss.NewStyle().Foreground(colLocalHi).Bold(true)
	stNameHidden = lipgloss.NewStyle().Foreground(colDim)
	stDir        = lipgloss.NewStyle().Foreground(colMuted)
	stDirSel     = lipgloss.NewStyle().Foreground(colDim)
	stDirHit     = lipgloss.NewStyle().Foreground(colLocal)
	stIconDir    = lipgloss.NewStyle().Foreground(colLocal)
	stIconFile   = lipgloss.NewStyle().Foreground(colDim)
	stEmptyState = lipgloss.NewStyle().Foreground(colMuted)

	stPaneTitle = lipgloss.NewStyle().Foreground(colBright).Bold(true)
	stPaneDim   = lipgloss.NewStyle().Foreground(colDim)

	stBar     = lipgloss.NewStyle().Background(colBar).Foreground(colDim)
	stBarText = lipgloss.NewStyle().Background(colBar).Foreground(colText)
	stBarWarn = lipgloss.NewStyle().Background(colBar).Foreground(colWarn)
	stBarErr  = lipgloss.NewStyle().Background(colBar).Foreground(colAlert)
	stMode    = lipgloss.NewStyle().Background(colLocal).Foreground(colBar).Bold(true)
)

// stCode colours highlighted code, in the spirit of Tokyo Night: muted
// enough to sit inside the noir frame, distinct enough to read structure at a
// glance. Plain code keeps the interface's own text colour.
var stCode = [...]lipgloss.Style{
	highlight.Plain:    lipgloss.NewStyle().Foreground(colText),
	highlight.Comment:  lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89")).Italic(true),
	highlight.String:   lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a")),
	highlight.Number:   lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9e64")),
	highlight.Keyword:  lipgloss.NewStyle().Foreground(lipgloss.Color("#bb9af7")),
	highlight.Type:     lipgloss.NewStyle().Foreground(lipgloss.Color("#2ac3de")),
	highlight.Constant: lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9e64")),
	highlight.Func:     lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")),
}

// The two icons every entry falls back to; icons by file type live in
// icons.go. The icons = false fallback, for terminals without a Nerd Font,
// comes with the config step.
const (
	iconDir  = "" // nf-fa-folder
	iconFile = "" // nf-fa-file
)
