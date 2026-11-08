package tui

import "charm.land/lipgloss/v2"

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

	stBar     = lipgloss.NewStyle().Background(colBar).Foreground(colDim)
	stBarText = lipgloss.NewStyle().Background(colBar).Foreground(colText)
	stBarWarn = lipgloss.NewStyle().Background(colBar).Foreground(colWarn)
	stBarErr  = lipgloss.NewStyle().Background(colBar).Foreground(colAlert)
	stMode    = lipgloss.NewStyle().Background(colLocal).Foreground(colBar).Bold(true)
)

// Icons from Nerd Fonts (Font Awesome set). Per-category icons and colours,
// and the icons = false fallback, come with the config step.
const (
	iconDir  = "" // nf-fa-folder
	iconFile = "" // nf-fa-file
)
