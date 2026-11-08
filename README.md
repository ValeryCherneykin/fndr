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
- **A real query line.** Move through it with `Ctrl+H` / `Ctrl+L`, fix a typo
  in the middle without retyping the rest.

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
| `Ctrl+N` / `Ctrl+P`, `↓` / `↑` | Move through the list |
| `PgDn` / `PgUp`, `End` / `Home` | Move by a page, to the end |
| `Enter` | Open a file · enter a folder |
| `-` · `Backspace` on an empty query | Go up a folder |
| `Ctrl+H` / `Ctrl+L`, `←` / `→` | Move through the query |
| `Ctrl+W` · `Ctrl+U` | Delete a word · delete to the start |
| `Ctrl+V` | Show or hide the preview (from 100 columns) |
| `Ctrl+E` | Quit and jump there |
| `Esc` | Clear the query, then quit |
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
  magic bytes (ELF, Mach-O), not by the `+x` bit, which FAT and exFAT set on
  every file.
- **Files can't drive your terminal.** Control characters in previews are
  replaced before printing, so a hostile file cannot clear the screen, move
  the cursor or retitle the window.
- **Never silent.** Whatever happens (opened, refused, failed) is said in the
  status bar.

---

## 🗺 Roadmap

- [x] Fuzzy search, browse, open, jump on exit
- [x] Preview pane, icons by type, query cursor
- [x] **fndr learns you** — frecency for files *and* folders you visit
- [x] Syntax highlighting and pictures in the preview
- [ ] **Instant start** — an on-disk index, caught up in the background
  (FSEvents on macOS)
- [ ] Stackable filters: `main .go >1mb @today`
- [ ] Oil-style editing: rename and delete by editing a buffer with Vim keys
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

---

<p align="center">
  <sub>MIT © Valery Cherneykin · built for the terminal, in Go</sub>
</p>
