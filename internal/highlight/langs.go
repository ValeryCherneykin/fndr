package highlight

import (
	"path/filepath"
	"strings"
)

// lang is everything the lexer knows about a language. Most languages
// differ only in these few tables, which is why one lexer serves them all.
type lang struct {
	lineComments []string // "//", "#", "--": a comment to the end of the line
	blocks       []block  // block comments and multi-line strings
	quotes       string   // characters that open a one-line string
	words        map[string]Kind
}

// block is a span that runs from open to close, possibly across lines:
// /* comments */, `raw strings`, """docstrings""".
type block struct {
	open, close string
	kind        Kind
}

// classify says what kind of span the word is. after is the text that
// follows it, so a name followed by "(" can be told apart as a call.
func (l *lang) classify(word, after string) Kind {
	// The word list comes first: "if (x)" has a "(" after it too, but "if"
	// is a keyword, not a call.
	if k, ok := l.words[word]; ok {
		return k
	}
	if strings.HasPrefix(after, "(") {
		return Func
	}
	return Plain
}

// langFor picks the language of a file by its whole name, then by its
// extension, or returns nil for a language it does not know.
func langFor(name string) *lang {
	base := filepath.Base(name)
	if l, ok := byName[base]; ok {
		return l
	}
	return byExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))]
}

// words builds a word table from space-separated lists, one per kind.
func words(keywords, types, constants string) map[string]Kind {
	m := make(map[string]Kind)
	for kind, list := range map[Kind]string{Keyword: keywords, Type: types, Constant: constants} {
		for w := range strings.FieldsSeq(list) {
			m[w] = kind
		}
	}
	return m
}

// anyCase adds the upper-case form of every word: SQL is written both ways.
func anyCase(m map[string]Kind) map[string]Kind {
	for w, k := range m {
		m[strings.ToUpper(w)] = k
	}
	return m
}

var (
	cComment  = block{"/*", "*/", Comment}
	hashLine  = []string{"#"}
	slashLine = []string{"//"}
)

var golang = &lang{
	lineComments: slashLine,
	blocks:       []block{cComment, {"`", "`", String}},
	quotes:       `"'`,
	words: words(
		"break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
		"any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr",
		"true false nil iota",
	),
}

var rust = &lang{
	lineComments: slashLine,
	blocks:       []block{cComment},
	quotes:       `"`, // not "'": it also marks lifetimes, as in &'a str
	words: words(
		"as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return static struct super trait type unsafe use where while",
		"bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize String Vec Option Result Box Self self",
		"true false None Some Ok Err",
	),
}

var python = &lang{
	lineComments: hashLine,
	blocks:       []block{{`"""`, `"""`, String}, {`'''`, `'''`, String}},
	quotes:       `"'`,
	words: words(
		"and as assert async await break case class continue def del elif else except finally for from global if import in is lambda match nonlocal not or pass raise return try while with yield",
		"bool bytes dict float int list object set str tuple self cls",
		"True False None",
	),
}

var javascript = &lang{
	lineComments: slashLine,
	blocks:       []block{cComment, {"`", "`", String}},
	quotes:       `"'`,
	words: words(
		"as async await break case catch class const continue default delete do else enum export extends finally for from function if implements import in instanceof interface let new of readonly return static super switch this throw try type typeof var void while yield",
		"any boolean never number object string unknown",
		"true false null undefined NaN",
	),
}

var clike = &lang{
	lineComments: slashLine,
	blocks:       []block{cComment},
	quotes:       `"'`,
	words: words(
		"auto break case catch class const constexpr continue default delete do else enum extern for goto if inline namespace new override private protected public return sizeof static struct switch template this throw try typedef typename union using virtual volatile while",
		"bool char double float int long short signed size_t unsigned void",
		"true false NULL nullptr",
	),
}

var java = &lang{
	lineComments: slashLine,
	blocks:       []block{cComment},
	quotes:       `"'`,
	words: words(
		"abstract assert break case catch class continue default do else enum extends final finally for if implements import instanceof interface new package private protected public record return static super switch synchronized this throw throws try var void volatile while",
		"boolean byte char double float int long short String",
		"true false null",
	),
}

var lua = &lang{
	lineComments: []string{"--"},
	// "--[[" must be tried before the "--" line comment: blocks come first.
	blocks: []block{{"--[[", "]]", Comment}, {"[[", "]]", String}},
	quotes: `"'`,
	words: words(
		"and break do else elseif end for function goto if in local not or repeat return then until while",
		"",
		"true false nil",
	),
}

var shell = &lang{
	lineComments: hashLine,
	quotes:       `"'`,
	words: words(
		"alias case do done elif else esac export fi for function if in local readonly return source then unset until while",
		"",
		"true false",
	),
}

var sql = &lang{
	lineComments: []string{"--"},
	blocks:       []block{cComment},
	quotes:       `'"`,
	words: anyCase(words(
		"add all alter and as asc begin between by case column commit create default delete desc distinct drop else end exists foreign from group having if in index inner insert into is join key left like limit not offset on or order outer primary references returning right rollback select set table then union unique update values when where",
		"bigint boolean char date integer int jsonb numeric serial text timestamp timestamptz uuid varchar",
		"true false null",
	)),
}

var data = &lang{ // YAML, TOML, INI, .env: comments, strings, values
	lineComments: hashLine,
	quotes:       `"'`,
	words:        words("", "", "true false null yes no on off"),
}

var json = &lang{
	quotes: `"`,
	words:  words("", "", "true false null"),
}

var dockerfile = &lang{
	lineComments: hashLine,
	quotes:       `"'`,
	words:        words("ADD ARG AS CMD COPY ENTRYPOINT ENV EXPOSE FROM HEALTHCHECK LABEL ONBUILD RUN SHELL STOPSIGNAL USER VOLUME WORKDIR", "", ""),
}

var makefile = &lang{
	lineComments: hashLine,
	quotes:       `"'`,
	words:        words("define else endef endif export ifdef ifeq ifndef ifneq include override", "", ""),
}

// byName matches whole file names, for files known by name, not extension.
var byName = map[string]*lang{
	"Dockerfile": dockerfile, "Containerfile": dockerfile,
	"Makefile": makefile, "makefile": makefile, "GNUmakefile": makefile,
	".zshrc": shell, ".bashrc": shell, ".profile": shell, ".zprofile": shell,
	".env": data, "go.mod": golang, "go.sum": data,
}

// byExt matches lower-case extensions, without the dot.
var byExt = map[string]*lang{
	"go": golang,
	"rs": rust,
	"py": python,
	"js": javascript, "mjs": javascript, "cjs": javascript, "jsx": javascript,
	"ts": javascript, "tsx": javascript,
	"c": clike, "h": clike, "cc": clike, "cpp": clike, "cxx": clike, "hpp": clike,
	"java": java, "kt": java,
	"lua": lua,
	"sh":  shell, "bash": shell, "zsh": shell, "fish": shell,
	"sql":  sql,
	"yaml": data, "yml": data, "toml": data, "ini": data, "conf": data, "env": data,
	"json": json,
	"mk":   makefile,
}
