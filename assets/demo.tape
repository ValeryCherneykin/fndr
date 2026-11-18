# Run from the repository root with vhs assets/demo.tape.
Output assets/demo.gif

Set Shell bash
Set FontFamily "Iosevka Nerd Font"
Set FontSize 18
Set Width 1360
Set Height 720
Set Padding 20
Set Theme {"name":"fndr","background":"#100d16","foreground":"#d4d4d8","cursor":"#a18bff","selection":"#33264a"}

Hide
Set TypingSpeed 2ms
Type `D="$(mktemp -d)" && D="$(cd "$D" && pwd -P)" || exit; trap 'find "$D" -depth -delete' EXIT; mkdir -p "$D/dev/fndr" "$D/.config/fndr"; git archive HEAD | tar -x -C "$D/dev/fndr" || exit; go build -o "$D/fndr" ./cmd/fndr || exit; : > "$D/.config/fndr/config"; export HOME="$D" COLORTERM=truecolor CLICOLOR_FORCE=1 TERM=xterm-256color; unset NO_COLOR GHOSTTY_RESOURCES_DIR KITTY_WINDOW_ID ITERM_SESSION_ID TMUX TERM_PROGRAM XDG_STATE_HOME XDG_CACHE_HOME XDG_CONFIG_HOME XDG_DATA_HOME; cd "$HOME/dev/fndr" || exit; clear; "$D/fndr"`
Enter
Sleep 5s
Set TypingSpeed 100ms
Show
Sleep 1s

Type "rename.go"
Sleep 450ms
Ctrl+V
Sleep 2s
Ctrl+V
Ctrl+U
Sleep 500ms

Ctrl+O
Sleep 750ms
Type "GoNOTES.md"
Escape
Sleep 300ms
Type ":w"
Enter
Sleep 1700ms
Type "y"
Sleep 1300ms
