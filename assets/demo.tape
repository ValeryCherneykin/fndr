# The README demo. Record it from the repository root:
#
#	make install && vhs assets/demo.tape
#
# It browses fndr's own source, so the GIF always shows real files.

Output assets/demo.gif

Set Shell zsh
Set FontFamily "Iosevka Nerd Font"
Set FontSize 18
Set Width 1400
Set Height 760
Set Padding 24
Set WindowBar Colorful
Set TypingSpeed 80ms
Set Theme { "name": "fndr-noir", "background": "#09090b", "foreground": "#d4d4d8", "cursor": "#38bdf8", "selection": "#27272a", "black": "#18181b", "brightBlack": "#52525b", "red": "#ef4444", "brightRed": "#f87171", "green": "#22c55e", "brightGreen": "#4ade80", "yellow": "#eab308", "brightYellow": "#facc15", "blue": "#38bdf8", "brightBlue": "#7dd3fc", "magenta": "#a78bfa", "brightMagenta": "#c4b5fd", "cyan": "#22d3ee", "brightCyan": "#67e8f9", "white": "#d4d4d8", "brightWhite": "#fafafa" }

# Set up off camera:
#   - the shell function, so Ctrl+E can change directory;
#   - a fresh, empty history and index (they live in $HOME, unless the
#     XDG variables move them out): the demo must not show, or depend on,
#     the habits or files of whoever records it, nor overwrite them;
#   - no hints of the recording machine's terminal: VHS cannot draw kitty
#     graphics, and fndr would otherwise trust the inherited variables;
#   - a fresh clone in a throwaway $HOME: the demo shows exactly what is on
#     GitHub, nothing of the machine it is recorded on (local notes, build
#     output, .DS_Store), and paths read "~/dev/fndr" for anyone;
#   - a prompt that shows paths from "~", so no user name is ever on screen.
Hide
Type `unset GHOSTTY_RESOURCES_DIR KITTY_WINDOW_ID TMUX TERM_PROGRAM XDG_STATE_HOME XDG_CACHE_HOME; export PATH="$HOME/go/bin:$PATH"; D="$(cd "$(mktemp -d)" && pwd -P)"; git clone -q "$PWD" "$D/dev/fndr"; export HOME="$D"; cd ~/dev/fndr; PS1='%F{cyan}%~%f ❯ '; eval "$(fndr init zsh)"; clear`
Enter
Sleep 1s
Show

Sleep 700ms
Type "fndr"
Sleep 300ms
Enter
Sleep 1s

# 1. Search, with the preview on: code is highlighted.
Ctrl+V
Sleep 500ms
Type "score"
Sleep 1.8s
Ctrl+N
Sleep 1.5s

# 2. A trailing slash finds folders only; the preview shows what is inside.
Ctrl+U
Type "internal/"
Sleep 1.5s

# 3. Go in, walk down to tui, go in, and jump: the shell lands there.
Enter
Sleep 1.2s
Down 8
Sleep 1.5s
Enter
Sleep 1.5s
Ctrl+E
Sleep 1.2s

# 3b. Search starts in the folder you are in: README.md is above tui, so
#     LOCAL finds nothing. Tab searches the whole home (GLOBAL, in violet)
#     with the same query. With the query cleared, GLOBAL shows the places
#     you go most: internal and tui, just visited. Esc steps back: GLOBAL,
#     then fndr.
Type "fndr"
Enter
Sleep 1s
Type "readme"
Sleep 1.5s
Tab
Sleep 2.5s
Ctrl+U
Sleep 2.5s
Escape
Sleep 400ms
Escape
Sleep 800ms

# 4. Back where we started, open fndr again: it remembers. The places you
#    go are on top, the strongest selected; Enter takes you there.
Type "cd ../.."
Enter
Sleep 600ms
Type "fndr"
Enter
Sleep 1s
Ctrl+V
Sleep 2.8s
Enter
Sleep 1.5s
Ctrl+E
Sleep 800ms
Type "ls"
Enter
Sleep 2.5s

# 5. Edit the folder as text. A deleted line goes to the trash, an edited
#    one is a rename, a new one is created with its folders; :w shows the
#    plan and y applies it. The log is made off camera to be thrown away.
Hide
Type "cd ~/dev/fndr && touch debug.log && clear"
Enter
Sleep 500ms
Show
Type "fndr"
Enter
Sleep 1s
Ctrl+O
Sleep 1.5s
Type "3j"
Sleep 600ms
Type "dd"
Sleep 800ms
Type "2j"
Sleep 400ms
Type "A.md"
Escape
Sleep 800ms
Type "Go"
Type "notes/ideas.md"
Escape
Sleep 800ms
Type ":w"
Enter
Sleep 2.5s
Type "y"
Sleep 2s

# 6. Back in search: find it, Ctrl+R, give it a new path, Enter.
Escape
Sleep 500ms
Type "ideas"
Sleep 1s
Ctrl+R
Sleep 1.2s
Ctrl+W
Type "docs/ideas.md"
Sleep 800ms
Enter
Sleep 2.5s
