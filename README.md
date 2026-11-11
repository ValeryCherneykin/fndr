<p align="center">
  <img src="assets/logo.webp" alt="fndr" width="100%" />
</p>

<h1 align="center">fndr</h1>

<p align="center">
  <strong>A search-first file manager for the terminal.</strong><br>
  <em>Don't walk the tree. Type, and it's there.</em>
</p>

<p align="center">
  <a href="https://github.com/ValeryCherneykin/fndr/releases"><img src="https://img.shields.io/github/v/release/ValeryCherneykin/fndr?style=flat-square&color=38bdf8&label=release" alt="Release"></a>
  <a href="https://github.com/ValeryCherneykin/fndr/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/ValeryCherneykin/fndr/ci.yml?branch=main&style=flat-square&label=CI" alt="CI"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/ValeryCherneykin/fndr?style=flat-square&color=00ADD8" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-a78bfa?style=flat-square" alt="License"></a>
</p>

<p align="center">
  <img src="assets/demo.gif" alt="fndr demo" width="100%" />
</p>

---

## 🧠 Why fndr

Every file manager asks you to **navigate**: open a folder, scan it, open
the next one, scan again. But you almost always know the name of what you
want. fndr starts from that.

Open it and type. Results appear with every keystroke, ranked the way fzf
ranks them, across the whole tree under you. Hit **Enter** and the file opens
in your editor. Hit **Ctrl+E** and your shell is standing in that folder.

| | **fndr** | yazi / ranger / nnn | fzf | Finder |
|---|:---:|:---:|:---:|:---:|
| Type to find anything below you | ✅ | ❌ per folder | ✅ | ✅ Spotlight |
| Lives in the terminal | ✅ | ✅ | ✅ | ❌ |
| Browse folders like a file manager | ✅ | ✅ | ❌ | ✅ |
| Preview while you move | ✅ | ✅ | ⚙️ via flags | ✅ |
| Leave the shell where you ended up | ✅ `Ctrl+E` | ⚙️ wrapper | ❌ | ❌ |
| Zero-config, single static binary | ✅ | ✅ | ✅ | ➖ |

---

## ⚡ Features

- **Search-first.** Fuzzy matching with fzf-style scoring: word starts,
  camelCase humps and path separators win. Matched letters are highlighted.
- **Your whole home, one key away.** Search starts in the folder you are
  in; `Tab` widens it to everything under `~`, with the same query, and
  back. The home is indexed the first time you ask, in the background.
- **Only what you would look for.** Caches, package stores, `node_modules`,
  hidden folders, app bundles and the folders a project's `.gitignore`
  names (`bin/`, `dist/`, `target/`) are listed, so you can find them by
  name and go in, but their insides stay out of the results. Ignored
  *files*, like `.env`, stay searchable. On a real home
  that is 88 thousand entries instead of 760 thousand.
- **Browse when you want to.** An empty query is a plain folder view:
  folders first, hidden files dimmed. `Enter` goes in, `-` goes up, and
  search narrows to wherever you are.
- **`internal/`** — end the query with a slash and only folders come back.
- **fndr learns you.** Folders you enter, files you open and places you jump
  to are remembered, frecency-style, as in zoxide. Search ranks your habits
  first, and an empty query offers the places you usually go from here,
  the strongest one selected: open fndr, press Enter, you're there.
- **Preview pane** (`Ctrl+V`). Code with syntax highlighting (Go, Rust,
  Python, JS/TS, C/C++, Java, Lua, shell, SQL, YAML/TOML/JSON and more),
  folders with their contents, **pictures as pictures** in kitty-compatible
  terminals (Ghostty, kitty; tmux works too), and a card for everything else:
  *PDF document · 2.1 MB · 8 Nov 2026*. Loaded off the UI thread with a 60 ms
  settle delay and a cache, so holding `↓` never lags.
- **Icons by file type.** ~150 types by name and extension (`Dockerfile`,
  `go.mod`, `.rs`, `.png`…), in the same glyphs and colours as
  nvim-web-devicons, so a file looks the same here as in your editor.
- **Opens things the right way.** Text goes to `$VISUAL` / `$EDITOR` (then
  nvim, vim, vi) and fndr comes back with your query intact; everything else
  goes to `open` / `xdg-open` / `wslview`.
- **Jump on exit.** `Ctrl+E` quits and `cd`s your shell into the selected
  folder, or into the folder of the selected file. Search + jump is a zoxide
  that already knows your whole disk.
- **Edit the folder as text** (`Ctrl+O`), as in Oil.nvim. See below.
- **Rename or move what you found** (`Ctrl+R`). The query line turns into
  the entry's path: change the name, or the folders in it, and press
  `Enter`. Missing folders are made, and nothing is ever overwritten.
- **A real query line.** Move through it with `Ctrl+H` / `Ctrl+L`, fix a typo
  in the middle without retyping the rest.

### ✏️ Edit the folder as text

`Ctrl+O` turns the folder into a buffer you edit with Vim keys:

| In the buffer | On disk |
|---|---|
| change a line | rename |
| `dd` a line | move to the trash |
| a new line `notes/ideas.md` | create it, with its folders |
| a line ending in `/` | create a folder |

Nothing touches the disk until you write. `:w` shows the plan, with
trashed entries in red and how much a folder holds (`trash node_modules/
(10000+ items)`), and `y` applies it. Afterwards the buffer is read again
from disk, and the status bar says what was done.

The keys are Vim's: `hjkl`, `0 ^ $ gg G`, counts (`3j`, `2dd`), `i a I A
o O`, `x dd D cc C s S yy p P`, `u` to undo and `U` to redo. Pasting a
list of names creates that many files. `g.` shows hidden entries.
`Enter`, `-` and `_` go into a folder, up a folder, and back to where
fndr started.

`Esc` (or `Ctrl+O` again) goes back to search. `:q`, `:wq` and `ZZ` quit
fndr, as they quit Vim. With unsaved changes, fndr won't leave the buffer:
write them with `:w`, undo them with `u`, or quit without them with `:q!`.

---

## 🚀 Install

### Homebrew (macOS, Linux)

```bash
brew install --cask ValeryCherneykin/fndr/fndr
```

### Go

```bash
go install github.com/ValeryCherneykin/fndr/cmd/fndr@latest
```

### Shell integration

A program cannot change its parent shell's directory, so `Ctrl+E` needs a
two-line shell function. Add this to `~/.zshrc`:

```zsh
eval "$(fndr init zsh)"
```

Now `fndr` is a function that runs the binary and `cd`s where you jumped.
`Esc` and `Ctrl+C` leave you exactly where you were. *(bash and fish are on
the roadmap.)*

Icons need a [Nerd Font](https://www.nerdfonts.com/).

---

## ⌨️ Keys

| Key | Action |
|---|---|
| *type* | Search everything below the current folder |
| `Tab` | Search your whole home instead (GLOBAL), and back; the query stays |
| `Ctrl+N` / `Ctrl+P`, `↓` / `↑` | Move through the list |
| `PgDn` / `PgUp`, `End` / `Home` | Move by a page, to the end |
| `Enter` | Open a file · enter a folder |
| `-` · `Backspace` on an empty query | Go up a folder |
| `Ctrl+H` / `Ctrl+L`, `←` / `→` | Move through the query |
| `Ctrl+W` · `Ctrl+U` | Delete a word · delete to the start |
| `Ctrl+V` | Show or hide the preview (from 100 columns) |
| `Ctrl+O` | Edit the folder as text with Vim keys (see above); `Esc` or `Ctrl+O` goes back, `:q` quits |
| `Ctrl+R` | Rename or move the selected entry: edit its path, `Enter`; missing folders are made, nothing is ever overwritten |
| `Ctrl+E` | Quit and jump there |
| `Esc` | Clear the query, then leave GLOBAL, then quit |
| `Ctrl+C` | Quit |

---

## 🏎 Why it's fast

Speed is feature number one: any change that makes typing or scrolling
noticeably slower is treated as a bug.

```
             crawl ──► batches of 1024 ──► flat []Entry (40 B each)
                                                 │  immutable snapshots:
                                                 │  search never takes a lock
   keystroke ──► cancel stale search ──► fuzzy filter on all cores
                                         (ASCII fast path, 0 allocs)
                                                 │
                                       fzf-style scoring ──► top-K heap
```

- **A flat index, not a tree.** Every file is one 40-byte entry in a slice:
  cache-friendly, trivially split across cores.
- **Two-pass matching.** A cheap subsequence check throws out almost
  everything; only survivors are scored.
- **Don't crawl what nobody searches.** The fastest crawl is the one that
  never happens: skipping caches and package stores took a real home from
  26 s to 0.6 s.
- **Read folders in parallel.** A crawl is mostly waiting on the disk,
  one `lstat` per file; eight workers keep it busy where one would ask for
  a file at a time. The same home: 0.6 s down to 0.19 s.
- **Never block the UI.** Crawling, searching and previews all run off the UI
  goroutine. A newer keystroke cancels the older search; stale replies are
  dropped by sequence number.

| Files indexed | Search time |
|---:|---:|
| 100,000 | **~1.1 ms** |
| 1,000,000 | **~8.8 ms** |

*Measured with `go test -bench` on a laptop; your disk and CPU will differ.*

---

## 🛡 Safe by default

- **Enter never runs a program.** Native binaries are recognized by their
  magic bytes (ELF, Mach-O, Windows `.exe`), not by the `+x` bit, which FAT
  and exFAT set on every file.
- **Files can't drive your terminal.** Not their contents, not their names:
  a file named with an escape sequence cannot clear the screen, write your
  clipboard or retitle the window, and an invisible bidi character cannot
  make a program called `invoice<U+202E>fdp.exe` read as `invoiceexe.pdf`.
- **No picture bombs.** A picture's size is read from its header first; a
  tiny file claiming 20000×20000 gets an info card, not gigabytes of memory.
- **Nothing is deleted for good, nothing is overwritten.** Deleting moves to
  the trash, and only after the plan is shown and confirmed. New files are
  created with `O_EXCL`: if the name is taken, nothing happens.
- **Several fndr, one history.** Open it in every tmux pane: visits add up
  under a file lock instead of the last one to exit erasing the others.
- **Never silent.** Whatever happens (opened, refused, failed) is said in the
  status bar.

---

## 🗺 Roadmap

- [x] Fuzzy search, browse, open, jump on exit
- [x] Preview pane, icons by type, query cursor
- [x] **fndr learns you** — frecency for files *and* folders you visit
- [x] Syntax highlighting and pictures in the preview
- [x] **Search the whole home** with `Tab`, junk left out
- [x] Oil-style editing: rename, trash and create by editing a buffer with
  Vim keys; rename or move any search result
- [ ] **Instant start** — an on-disk index, caught up in the background
  by checking which folders changed
- [ ] Stackable filters: `main .go >1mb @today`
- [ ] Config: rebind any key, choose your editor
- [ ] bash and fish integration

---

## 🛠 Development

Requires Go 1.26.5+. The core packages use only the standard library; the
UI is [Bubble Tea](https://github.com/charmbracelet/bubbletea).

```bash
make build   # bin/fndr
make check   # exactly what CI runs: tidy, vet, lint, tests with -race
make         # list every command
```

Contributions are welcome. Please run `make check` before opening a PR.

Found a bug? [Open a bug report](https://github.com/ValeryCherneykin/fndr/issues/new?template=bug_report.yml).
fndr never sends anything on its own, so a report is the only way I hear of it.

---

<p align="center">
  <sub>MIT © Valery Cherneykin · built for the terminal, in Go</sub>
</p>
