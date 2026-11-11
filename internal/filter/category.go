package filter

import "strings"

// category is a family of files known by their extension, one bit each,
// so the categories of a tag are one set: code|doc looks the extension up
// once. A file's kind could be told better by its first bytes, as Enter
// does (opener.Classify), but a tag is tested on every entry of the index
// for every key typed: opening files there is out of the question.
type category uint8

const (
	audio category = 1 << iota
	image
	video
	doc
	code
	archive
)

// categories maps a tag to its category.
var categories = map[string]category{
	"audio":   audio,
	"image":   image,
	"video":   video,
	"doc":     doc,
	"code":    code,
	"archive": archive,
}

// extCategory maps an extension, in lower case and without its dot, to its
// category. Only extensions that mean one thing: .ts is code here, not
// an MPEG stream, since a developer has far more of the first.
var extCategory = func() map[string]category {
	m := map[string]category{}
	add := func(c category, exts string) {
		for _, e := range strings.Fields(exts) {
			m[e] = c
		}
	}
	add(audio, "mp3 m4a aac flac wav aiff aif ogg oga opus wma alac mid midi")
	add(image, "png jpg jpeg gif webp heic heif bmp tif tiff svg ico icns avif raw cr2 cr3 nef arw dng psd")
	add(video, "mp4 m4v mov mkv avi webm wmv flv mpg mpeg 3gp")
	add(doc, "pdf doc docx odt rtf txt md markdown rst org tex epub pages key numbers xls xlsx ods csv ppt pptx odp")
	add(code, "go rs c h cc cpp cxx hpp hh m mm swift java kt kts scala clj ex exs erl hs ml py rb php pl lua r jl dart "+
		"js mjs cjs jsx ts tsx vue svelte html htm css scss sass less sql sh bash zsh fish ps1 vim el zig nim cs fs vb "+
		"json yaml yml toml xml ini proto graphql gradle cmake mk")
	add(archive, "zip tar gz tgz bz2 tbz xz txz zst 7z rar dmg iso pkg deb rpm jar war apk")
	return m
}()

const noCategory category = 0

// categoryOf returns the category of a file by its name's last extension.
// The extension is lowered into a buffer on the stack: a map index with
// string(bytes) does not allocate.
func categoryOf(name string) category {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 { // no extension, or a name like .zshrc
		return noCategory
	}
	ext := name[dot+1:]
	var buf [8]byte
	if len(ext) > len(buf) {
		return noCategory // longer than any extension above
	}
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		buf[i] = c
	}
	return extCategory[string(buf[:len(ext)])]
}
