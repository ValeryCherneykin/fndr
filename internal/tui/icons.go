package tui

import (
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// Icons by file type, from Nerd Fonts. Glyphs and colours follow
// nvim-web-devicons, so a file looks the same in fndr as in the editor it
// opens in.
//
// Only the icon is coloured, in the brand colour of its type (Go cyan, Rust
// orange): that is what makes a type recognizable before the name is read.
// Names stay grey, so the screen stays "noir" and the eye still lands on
// names first.
//
// A file is matched by its whole name first (Dockerfile, go.mod), then by
// its extension, then gets the plain file icon.

// icon is a glyph in its colour.
type icon struct {
	glyph string
	color string
}

// iconsByName matches whole file names: files whose name says more than
// their extension, or that have none.
var iconsByName = rendered(map[string]icon{
	"Dockerfile":          {"󰡨", "#458ee6"},
	"dockerfile":          {"󰡨", "#458ee6"},
	"docker-compose.yml":  {"󰡨", "#458ee6"},
	"compose.yaml":        {"󰡨", "#458ee6"},
	".dockerignore":       {"󰡨", "#458ee6"},
	"Makefile":            {"", "#6d8086"},
	"makefile":            {"", "#6d8086"},
	"Justfile":            {"", "#6d8086"},
	"justfile":            {"", "#6d8086"},
	"CMakeLists.txt":      {"", "#dce3eb"},
	"go.mod":              {"", "#00add8"},
	"go.sum":              {"", "#00add8"},
	"package.json":        {"", "#e8274b"},
	"package-lock.json":   {"", "#7a0d21"},
	"tsconfig.json":       {"", "#519aba"},
	".gitignore":          {"", "#f54d27"},
	".gitattributes":      {"", "#f54d27"},
	".gitmodules":         {"", "#f54d27"},
	"LICENSE":             {"", "#d0bf41"},
	"license":             {"", "#d0bf41"},
	"README":              {"󰂺", "#ededed"},
	"README.md":           {"󰂺", "#ededed"},
	".env":                {"", "#faf743"},
	".zshrc":              {"", "#89e051"},
	".bashrc":             {"", "#89e051"},
	".vimrc":              {"", "#019833"},
	".editorconfig":       {"", "#fff2f2"},
	"go.work":             {"", "#00add8"},
	"docker-compose.yaml": {"󰡨", "#458ee6"},
	"compose.yml":         {"󰡨", "#458ee6"},
	"GNUmakefile":         {"", "#6d8086"},
	"Cargo.toml":          {"", "#dea584"},
	"Cargo.lock":          {"", "#dea584"},
})

// iconsByExt matches lower-case extensions, without the dot.
var iconsByExt = rendered(map[string]icon{
	// Programming languages.
	"go":    {"", "#00add8"},
	"rs":    {"", "#dea584"},
	"py":    {"", "#ffbc03"},
	"js":    {"", "#cbcb41"},
	"mjs":   {"", "#f1e05a"},
	"ts":    {"", "#519aba"},
	"tsx":   {"", "#1354bf"},
	"jsx":   {"", "#20c2e3"},
	"lua":   {"", "#51a0cf"},
	"c":     {"", "#599eff"},
	"h":     {"", "#a074c4"},
	"cpp":   {"", "#519aba"},
	"hpp":   {"", "#a074c4"},
	"cc":    {"", "#f34b7d"},
	"java":  {"", "#cc3e44"},
	"kt":    {"", "#7f52ff"},
	"swift": {"", "#e37933"},
	"rb":    {"", "#701516"},
	"php":   {"", "#a074c4"},
	"cs":    {"󰌛", "#596706"},
	"zig":   {"", "#f69a1b"},
	"nim":   {"", "#f3d400"},
	"ex":    {"", "#a074c4"},
	"exs":   {"", "#a074c4"},
	"erl":   {"", "#b83998"},
	"hs":    {"", "#a074c4"},
	"ml":    {"", "#e37933"},
	"scala": {"", "#cc3e44"},
	"dart":  {"", "#03589c"},
	"r":     {"󰟔", "#2266ba"},
	"jl":    {"", "#a270ba"},
	"vim":   {"", "#019833"},
	// Shells and query languages.
	"sh":      {"", "#4d5a5e"},
	"bash":    {"", "#89e051"},
	"zsh":     {"", "#89e051"},
	"fish":    {"", "#4d5a5e"},
	"ps1":     {"󰨊", "#4273ca"},
	"sql":     {"", "#dad8d8"},
	"graphql": {"", "#e535ab"},
	// Web.
	"html":   {"", "#e44d26"},
	"css":    {"", "#663399"},
	"scss":   {"", "#f55385"},
	"sass":   {"", "#f55385"},
	"vue":    {"", "#8dc149"},
	"svelte": {"", "#ff3e00"},
	// Documents.
	"md":    {"", "#dddddd"},
	"mdx":   {"", "#519aba"},
	"txt":   {"󰈙", "#89e051"},
	"org":   {"", "#77aa99"},
	"tex":   {"", "#3d6117"},
	"pdf":   {"", "#b30b00"},
	"doc":   {"󰈬", "#185abd"},
	"docx":  {"󰈬", "#185abd"},
	"xls":   {"󰈛", "#207245"},
	"xlsx":  {"󰈛", "#207245"},
	"csv":   {"", "#89e051"},
	"ppt":   {"󰈧", "#cb4a32"},
	"pptx":  {"󰈧", "#cb4a32"},
	"log":   {"󰌱", "#dddddd"},
	"diff":  {"", "#41535b"},
	"patch": {"", "#41535b"},
	// Configuration and data.
	"json": {"", "#cbcb41"},
	"yaml": {"", "#d70000"},
	"yml":  {"", "#d70000"},
	"toml": {"", "#9c4221"},
	"ini":  {"", "#6d8086"},
	"conf": {"", "#6d8086"},
	"cfg":  {"", "#6d8086"},
	"xml":  {"󰗀", "#e37933"},
	"env":  {"", "#faf743"},
	"lock": {"", "#bbbbbb"},
	"nix":  {"", "#7ebae4"},
	"tf":   {"", "#5f43e9"},
	// Images.
	"png":  {"", "#a074c4"},
	"jpg":  {"", "#a074c4"},
	"jpeg": {"", "#a074c4"},
	"gif":  {"", "#a074c4"},
	"webp": {"", "#a074c4"},
	"svg":  {"󰜡", "#ffb13b"},
	"ico":  {"", "#cbcb41"},
	"bmp":  {"", "#a074c4"},
	"psd":  {"", "#519aba"},
	// Audio and video.
	"mp3":  {"", "#00afff"},
	"wav":  {"", "#00afff"},
	"flac": {"", "#0075aa"},
	"ogg":  {"", "#0075aa"},
	"m4a":  {"", "#00afff"},
	"mp4":  {"", "#fd971f"},
	"mkv":  {"", "#fd971f"},
	"mov":  {"", "#fd971f"},
	"avi":  {"", "#fd971f"},
	"webm": {"", "#fd971f"},
	// Archives and disk images.
	"zip": {"", "#eca517"},
	"gz":  {"", "#eca517"},
	"tgz": {"", "#eca517"},
	"bz2": {"", "#eca517"},
	"xz":  {"", "#eca517"},
	"zst": {"", "#eca517"},
	"7z":  {"", "#eca517"},
	"rar": {"", "#eca517"},
	"iso": {"", "#d0bec8"},
	// Databases, fonts, binaries.
	"db":      {"", "#dad8d8"},
	"sqlite":  {"", "#dad8d8"},
	"sqlite3": {"", "#dad8d8"},
	"ttf":     {"", "#ececec"},
	"otf":     {"", "#ececec"},
	"woff":    {"", "#ececec"},
	"woff2":   {"", "#ececec"},
	"exe":     {"", "#9f0500"},
	"dll":     {"", "#4d2c0b"},
	"so":      {"", "#dcddd6"},
	"a":       {"", "#dcddd6"},
	"o":       {"", "#9f0500"},
	"wasm":    {"", "#5c4cdb"},
	"bin":     {"", "#9f0500"},
	// Formats with no glyph of their own borrow their family's.
	"tar":   {"", "#eca517"},
	"dmg":   {"", "#d0bec8"},
	"pkg":   {"", "#eca517"},
	"deb":   {"", "#eca517"},
	"rpm":   {"", "#eca517"},
	"heic":  {"", "#a074c4"},
	"tiff":  {"", "#a074c4"},
	"dylib": {"", "#dcddd6"},
})

// rendered styles every icon once, at start-up, so drawing a row is a map
// lookup rather than a style render per row per frame.
func rendered(icons map[string]icon) map[string]string {
	out := make(map[string]string, len(icons))
	for k, ic := range icons {
		out[k] = lipgloss.NewStyle().Foreground(lipgloss.Color(ic.color)).Render(ic.glyph)
	}
	return out
}

var (
	iconDirStyled  = stIconDir.Render(iconDir)
	iconFileStyled = stIconFile.Render(iconFile)

	// iconHabitStyled marks a place shown because the user often goes
	// there: a clock (nf-md-history), in the scope accent.
	iconHabitStyled = stIconDir.Render("\U000F02DA")
)

// iconFor returns the styled icon for an entry: folder, whole name,
// extension, then the plain file icon.
func iconFor(name string, isDir bool) string {
	if isDir {
		return iconDirStyled // a folder named "backup.go" is still a folder
	}
	if ic, ok := iconsByName[name]; ok {
		return ic
	}
	// "PHOTO.JPG" and "photo.jpg" are the same kind of file; only the last
	// extension counts, so "archive.tar.gz" is a gzip archive.
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if ic, ok := iconsByExt[ext]; ok {
		return ic
	}
	return iconFileStyled
}
