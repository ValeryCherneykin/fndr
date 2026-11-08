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
Set TypingSpeed 90ms
Set Theme { "name": "fndr-noir", "background": "#09090b", "foreground": "#d4d4d8", "cursor": "#38bdf8", "selection": "#27272a", "black": "#18181b", "brightBlack": "#52525b", "red": "#ef4444", "brightRed": "#f87171", "green": "#22c55e", "brightGreen": "#4ade80", "yellow": "#eab308", "brightYellow": "#facc15", "blue": "#38bdf8", "brightBlue": "#7dd3fc", "magenta": "#a78bfa", "brightMagenta": "#c4b5fd", "cyan": "#22d3ee", "brightCyan": "#67e8f9", "white": "#d4d4d8", "brightWhite": "#fafafa" }

# Set up off camera: the shell function, so Ctrl+E can change directory.
Hide
# The prompt shows the path from "~", so no user name ever appears on screen.
Type `export PATH="$HOME/go/bin:$PATH" PS1='%F{cyan}%~%f ❯ ' && eval "$(fndr init zsh)" && clear`
Enter
Show

Sleep 800ms
Type "fndr"
Sleep 400ms
Enter
Sleep 1.2s

# 1. Search: a few letters find a file anywhere below.
Type "score"
Sleep 1.2s

# 2. The preview follows the selection.
Ctrl+V
Sleep 1.5s
Ctrl+N
Sleep 1.2s
Ctrl+P
Sleep 1s

# 3. A trailing slash finds folders only; the preview shows what is inside.
Ctrl+U
Type "internal/"
Sleep 1.5s
Ctrl+N
Sleep 1.5s

# 4. Browse: go in, look around, come back up.
Ctrl+U
Sleep 600ms
Type "tui/"
Sleep 800ms
Enter
Sleep 1.5s
Down 3
Sleep 1.2s
Type "-"
Sleep 1.2s

# 5. Search, then jump: the shell lands in the folder.
Type "shell"
Sleep 1.2s
# The prompt now shows where the jump landed; ls proves it.
Ctrl+E
Sleep 1s
Type "ls"
Enter
Sleep 2.5s
